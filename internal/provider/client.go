// Package provider contains the implementation of the Icotera i4850 Terraform provider,
// including the client which scrapes the router webpage, and the resource definitions.
package provider

import (
	"context"
	"fmt"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"sync"
)

// IcoteraClient interacts with the Icotera i4850 web interface
type IcoteraClient struct {
	RouterAddress string
	Username      string
	Password      string
	lock          sync.Mutex // Critical for hardware with single-session UIs
	AlertFound    bool       // some errors are reported by alerts so this needs to be passed back
	AlertMsg      string
}

// NewIcoteraClient initializes a new client for the Icotera router.
func NewIcoteraClient(routerAddress, user, pass string) (*IcoteraClient, error) {
	return &IcoteraClient{
		RouterAddress: routerAddress,
		Username:      user,
		Password:      pass,
	}, nil
}

// RunActions authenticates with the router and executes the provided chromedp actions
func (c *IcoteraClient) RunActions(ctx context.Context, actions ...chromedp.Action) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.IgnoreCertErrors, // Bypasses the "No Certificate" warning
		chromedp.NoSandbox,

		// if the terraform plan application times out, being able
		// to see the state of the webpage is extremely useful
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	defer cancel()

	taskCtx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	chromedp.ListenTarget(taskCtx, func(ev interface{}) {
		switch e := ev.(type) {
		// record alert dialogs, as some validation errors in the dhcp lease screen are reported through them
		case *page.EventJavascriptDialogOpening:
			c.AlertMsg = e.Message
			c.AlertFound = true
			go func() {
				_ = chromedp.Run(taskCtx, page.HandleJavaScriptDialog(true))
			}()

		case *runtime.EventConsoleAPICalled:
			for _, arg := range e.Args {
				// Unquote handles the "double quoted" strings often returned by CDP
				val := string(arg.Value)
				fmt.Printf("[JS Console %s] %s\n", e.Type, val)
			}

		case *runtime.EventExceptionThrown:
			fmt.Printf("[JS Exception] %s\n", e.ExceptionDetails.Text)
		}
	})

	return chromedp.Run(taskCtx, append([]chromedp.Action{
		chromedp.Navigate("https://" + c.RouterAddress + "/"),
		chromedp.WaitVisible(`input[value="Log in"]`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="username"]`, c.Username),
		chromedp.SendKeys(`input[name="password"]`, c.Password),
		chromedp.Click(`input[value="Log in"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`div.C_CSS_content_column`, chromedp.ByQuery),
	}, actions...)...)
}
