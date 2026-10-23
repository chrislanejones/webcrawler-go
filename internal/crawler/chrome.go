package crawler

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// newBrowser starts one headless Chrome for a whole capture run. Each page is
// then a new tab in it (chromedp.NewContext on the returned context), instead
// of a new Chrome process per page, which cost a browser launch on every
// page and kept one full browser in memory per worker.
//
// The browser hangs off context.Background, not the run's context, so a
// Ctrl-C lets the captures in flight finish. stop closes it.
func newBrowser() (browser context.Context, stop func(), err error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-web-security", true),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.WindowSize(1920, 1080),
	)
	opts = append(opts, chromeProcessOptions()...)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	stop = func() {
		browserCancel()
		allocCancel()
	}

	// Run with no actions starts the browser, so a missing Chrome is
	// reported once, up front, instead of as an error on every page.
	if err := chromedp.Run(browserCtx); err != nil {
		stop()
		return nil, func() {}, fmt.Errorf("starting Chrome: %w", err)
	}
	return browserCtx, stop, nil
}

// pageRender is what renderPage captured from one page.
type pageRender struct {
	pdf   []byte
	png   []byte
	links string // newline-separated hrefs from the rendered DOM
}

// renderPage opens pageURL in a new tab of the run's browser, lets it settle,
// and captures what was asked for. Page Capture and JSON Feed Capture share
// it, so the two modes render pages the same way.
//
// Settling takes about 6 seconds a page: a pause for JavaScript frameworks to
// start, a scroll to the bottom and back to trigger lazy loading, then waiting
// until the body text stops growing.
func renderPage(pageURL string, wantPNG, wantPDF, wantLinks bool) (pageRender, error) {
	var out pageRender

	ctx, cancel := chromedp.NewContext(captureBrowser)
	defer cancel()
	// 180s for slow, heavy pages
	ctx, cancel = context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	actions := []chromedp.Action{
		chromedp.Navigate(pageURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(2 * time.Second),
		chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight)`, nil),
		chromedp.Sleep(1 * time.Second),
		chromedp.Evaluate(`window.scrollTo(0, 0)`, nil),
		chromedp.Sleep(500 * time.Millisecond),
		chromedp.ActionFunc(waitForStableBody),
		chromedp.Sleep(1 * time.Second),
	}

	if wantLinks {
		actions = append(actions, chromedp.Evaluate(`
			Array.from(document.querySelectorAll('a[href]'))
				.map(a => a.href)
				.filter(href => href && !href.startsWith('javascript:') && !href.startsWith('mailto:') && !href.startsWith('tel:'))
				.join('\n')
		`, &out.links))
	}

	if wantPNG {
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, contentSize, _, _, _, err := page.GetLayoutMetrics().Do(ctx)
			if err != nil {
				return err
			}
			// Full-page viewport, capped to avoid memory issues
			width, height := int64(contentSize.Width), min(int64(contentSize.Height), 16384)
			err = emulation.SetDeviceMetricsOverride(width, height, 1, false).
				WithScreenOrientation(&emulation.ScreenOrientation{
					Type:  emulation.OrientationTypePortraitPrimary,
					Angle: 0,
				}).Do(ctx)
			if err != nil {
				return err
			}
			out.png, err = page.CaptureScreenshot().
				WithQuality(100).
				WithFromSurface(true).
				Do(ctx)
			return err
		}))
	}

	if wantPDF {
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			out.pdf, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithScale(1.0).
				WithPaperWidth(8.5).
				WithPaperHeight(11).
				WithMarginTop(0.4).
				WithMarginBottom(0.4).
				WithMarginLeft(0.4).
				WithMarginRight(0.4).
				WithDisplayHeaderFooter(false).
				WithGenerateDocumentOutline(false).
				Do(ctx)
			return err
		}))
	}

	err := chromedp.Run(ctx, actions...)
	return out, err
}

// waitForStableBody waits until the page's body text has stopped growing for
// 1.5 seconds, for pages that fill themselves in after load. It gives up after
// about 15 seconds and waits 2 more.
func waitForStableBody(ctx context.Context) error {
	var lastLength, stableCount int
	for range 30 {
		var currentLength int
		if err := chromedp.Evaluate(`document.body.innerText.length`, &currentLength).Do(ctx); err == nil {
			if currentLength > 500 && currentLength == lastLength {
				stableCount++
				if stableCount >= 3 {
					return nil
				}
			} else {
				stableCount = 0
			}
			lastLength = currentLength
		}
		if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
			return err
		}
	}
	return sleepCtx(ctx, 2*time.Second)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
