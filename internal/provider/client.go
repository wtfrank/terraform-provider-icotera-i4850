package provider

import (
	"context"
	"fmt"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"sync"
)

type IcoteraClient struct {
	RouterAddress string
	Username      string
	Password      string
	lock          sync.Mutex // Critical for hardware with single-session UIs
	AlertFound    bool       // some errors are reported by alerts so this needs to be passed back
	AlertMsg      string
}

func NewIcoteraClient(router_address, user, pass string) (*IcoteraClient, error) {
	return &IcoteraClient{
		RouterAddress: router_address,
		Username:      user,
		Password:      pass,
	}, nil
}

func (c *IcoteraClient) RunActions(ctx context.Context, actions ...chromedp.Action) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.IgnoreCertErrors, // Bypasses the "No Certificate" warning
		chromedp.NoSandbox,

                // if the terraform plan application times out, being able
                // to see the state of the webpage is extremely useful
		chromedp.Flag("headless", false),
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
