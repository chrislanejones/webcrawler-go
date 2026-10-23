package crawler

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// newBrowser starts one headless Chrome for a whole capture run. Each page is
// then a new tab in it (chromedp.NewContext on the returned context), instead
// of a new Chrome process per page, which cost a browser launch on every
// page and kept one full browser in memory per worker. chromedp opens each
// tab in its own window, so no tab is hidden behind another: a hidden tab
// gets no animation frames and a screenshot of it stalls.
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

	// Do with no steps starts the browser, so a missing Chrome is
	// reported once, up front, instead of as an error on every page.
	if err := chromedp.Do(browserCtx); err != nil {
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

	err := chromedp.Do(ctx,
		chromedp.Navigate(pageURL),
		chromedp.WaitReady(chromedp.CSS("body")),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate[chromedp.Void](`window.scrollTo(0, document.body.scrollHeight)`),
		chromedp.Sleep(1*time.Second),
		chromedp.Evaluate[chromedp.Void](`window.scrollTo(0, 0)`),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Func(waitForStableBody),
		chromedp.Sleep(1*time.Second),
	)
	if err != nil {
		return out, err
	}

	if wantLinks {
		out.links, err = chromedp.Run(ctx, chromedp.Evaluate[string](`
			Array.from(document.querySelectorAll('a[href]'))
				.map(a => a.href)
				.filter(href => href && !href.startsWith('javascript:') && !href.startsWith('mailto:') && !href.startsWith('tel:'))
				.join('\n')
		`))
		if err != nil {
			return out, err
		}
	}

	if wantPNG {
		if out.png, err = chromedp.Run(ctx, fullPageScreenshot); err != nil {
			return out, err
		}
	}

	if wantPDF {
		out.pdf, err = chromedp.Run(ctx, chromedp.PrintToPDF(
			chromedp.PDFPaper(chromedp.PaperLetter),
			chromedp.PDFMargins(0.4, 0.4, 0.4, 0.4),
			chromedp.PDFPrintBackground(),
			chromedp.PDFScale(1.0),
		))
	}
	return out, err
}

// fullPageScreenshot sizes the viewport to the whole page, capped at 16,384
// pixels tall to bound memory, and captures it as a PNG.
func fullPageScreenshot(ctx context.Context, t *chromedp.Target) ([]byte, error) {
	metrics, err := cdp.Call(ctx, t, page.GetLayoutMetrics, cdp.Empty{})
	if err != nil {
		return nil, err
	}
	size := metrics.CSSContentSize
	if size == nil {
		return nil, fmt.Errorf("page reported no content size")
	}

	_, err = cdp.Call(ctx, t, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{
		Width:             int64(size.Width),
		Height:            min(int64(size.Height), 16384),
		DeviceScaleFactor: 1,
		ScreenOrientation: &emulation.ScreenOrientation{
			Type:  emulation.ScreenOrientationTypePortraitPrimary,
			Angle: 0,
		},
	})
	if err != nil {
		return nil, err
	}

	shot, err := cdp.Call(ctx, t, page.CaptureScreenshot, page.CaptureScreenshotParams{
		FromSurface: new(true),
	})
	if err != nil {
		return nil, err
	}
	return shot.Data, nil
}

// waitForStableBody waits until the page's body text has stopped growing for
// 1.5 seconds, for pages that fill themselves in after load. It gives up after
// about 15 seconds and waits 2 more.
func waitForStableBody(ctx context.Context, t *chromedp.Target) error {
	var lastLength, stableCount int
	for range 30 {
		currentLength, err := chromedp.Evaluate[int](`document.body.innerText.length`)(ctx, t)
		if err == nil {
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
