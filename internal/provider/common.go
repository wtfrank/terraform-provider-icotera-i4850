package provider

import (
	"context"
	"fmt"
	"github.com/chromedp/chromedp"
	"log"
	"time"
)

func applyAndCheckErrorsActions(client *IcoteraClient, result *struct {
	IsError bool   `json:"isError"`
	Message string `json:"message"`
}) []chromedp.Action {
	return []chromedp.Action{
		chromedp.WaitVisible(`#btn_apply`, chromedp.ByID),
		chromedp.Click(`#btn_apply`, chromedp.ByID),

		// Router seems to do some validation at apply
		// for example, loopback with a port range selected
		// Have added terraform validators that replicate this
		// for better feedback
		chromedp.ActionFunc(func(_ context.Context) error {
			time.Sleep(300 * time.Millisecond)
			if client.AlertFound {
				log.Printf("router alert: %s", client.AlertMsg)
				return fmt.Errorf("halting: router alert: %s", client.AlertMsg)
			}
			return nil
		}),

		chromedp.WaitVisible(`#content_overlay_panel`, chromedp.ByID),

		chromedp.Evaluate(`(function() {
			const reportBox = document.querySelector('.C_CSS_MsgReportBox');
			if (reportBox && reportBox.innerText.trim().length > 0) {
				return {
					isError: true,
					message: reportBox.innerText.trim()
				};
			}
			return { isError: false, message: "" };
		})()`, result),

		chromedp.WaitVisible(`.C_CSS_flatbtn[value="Continue"]`, chromedp.ByQuery),
		chromedp.Sleep(500 * time.Millisecond),
		chromedp.WaitNotVisible(`.C_CSS_LoadingDiv`, chromedp.ByQuery),
		chromedp.Sleep(100 * time.Millisecond),
		chromedp.Click(`.C_CSS_flatbtn[value="Continue"]`, chromedp.ByQuery),
		chromedp.WaitNotVisible(`#content_overlay`, chromedp.ByID),
		chromedp.WaitNotVisible(`.C_CSS_LoadingDiv`, chromedp.ByQuery),
		// big cooldown to give the router time to breathe
		chromedp.Sleep(2000 * time.Millisecond),
	}
}
