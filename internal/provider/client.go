package provider

import (
	"context"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"sync"
)

type IcoteraClient struct {
	Endpoint   string
	Username   string
	Password   string
	lock       sync.Mutex // Critical for hardware with single-session UIs
	AlertFound bool       // some errors are reported by alerts so this needs to be passed back
	AlertMsg   string
}

func NewIcoteraClient(endpoint, user, pass string) (*IcoteraClient, error) {
	return &IcoteraClient{
		Endpoint: endpoint,
		Username: user,
		Password: pass,
	}, nil
}

// NavigateAndAuth handles the login flow and certificate errors
func (c *IcoteraClient) RunActions(ctx context.Context, actions ...chromedp.Action) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.IgnoreCertErrors, // Bypasses the "No Certificate" warning
		chromedp.NoSandbox,

		chromedp.Flag("headless", false),
		chromedp.Flag("disable-gpu", true),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	defer cancel()

	taskCtx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	chromedp.ListenTarget(taskCtx, func(ev interface{}) {
		if ev, ok := ev.(*page.EventJavascriptDialogOpening); ok {
			c.AlertMsg = ev.Message
			c.AlertFound = true
			go func() {
				_ = chromedp.Run(taskCtx, page.HandleJavaScriptDialog(true))
			}()
		}
	})

	// Full flow: Login -> Run desired Resource actions -> Logout
	return chromedp.Run(taskCtx, append([]chromedp.Action{
		chromedp.Navigate("https://" + c.Endpoint + "/"),
		chromedp.WaitVisible(`input[value="Log in"]`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="username"]`, c.Username),
		chromedp.SendKeys(`input[name="password"]`, c.Password),
		chromedp.Click(`input[value="Log in"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`div.C_CSS_content_column`, chromedp.ByQuery), // Ensure we are actually in
	}, actions...)...)
}
