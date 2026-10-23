package crawler

import (
	"context"
	"fmt"

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
