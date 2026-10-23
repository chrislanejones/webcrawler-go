//go:build !unix

package crawler

import "github.com/chromedp/chromedp"

// chromeProcessOptions has nothing to add outside Unix: Windows delivers
// Ctrl-C to console processes differently, and process groups are a Unix idea.
func chromeProcessOptions() []chromedp.ExecAllocatorOption {
	return nil
}
