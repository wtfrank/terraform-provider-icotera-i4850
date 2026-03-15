package provider

import (
	"context"
	"fmt"
	"github.com/chromedp/chromedp"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"log"
	"strings"
	"time"
)

var (
	_ resource.Resource                = &staticLeaseResource{}
	_ resource.ResourceWithConfigure   = &staticLeaseResource{}
	_ resource.ResourceWithImportState = &staticLeaseResource{}
)

type staticLeaseResourceModel struct {
	Hostname   types.String `tfsdk:"hostname"`
	MacAddress types.String `tfsdk:"mac_address"`
	IPAddress  types.String `tfsdk:"ip_address"`
	Enabled    types.Bool   `tfsdk:"enabled"`
	ID         types.String `tfsdk:"id"` // mac address
}

type staticLeaseResource struct {
	client *IcoteraClient
}

func (r *staticLeaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_static_lease"
}

func (r *staticLeaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `Static DHCP Leases (IPv4)

Allocate consistent IPv4 addresses to devices inside the network. Devices are identified by MAC address. Assigned addresses must be within the range of addresses defined by the IP address and netmask configured via the icotera-i4850_lan_settings resource or at Settings->LAN on the router webpage.`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The ID of the entry (mac address)",
				Computed:    true,
			},
			"hostname": schema.StringAttribute{
				Description: "The local hostname of the device",
				Required:    true},
			"mac_address": schema.StringAttribute{
				Description: "The mac address of the device that you wish to assign an ip address to",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(), // Forces destroy/create if MAC changes
				},
			},
			"ip_address": schema.StringAttribute{
				Description: "The IPv4 address that you wish to assign to the device. Must be within the range IP address and netmask configured at Settings->LAN",
				Required:    true},
			"enabled": schema.BoolAttribute{
				Description: "Whether this mapping is active",
				Optional:    true,
				Computed:    true},
		},
	}
}

func (r *staticLeaseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*IcoteraClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *IcoteraClient")
		return
	}

	r.client = client
}

func (r *staticLeaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// We use the MAC address as the import identifier
	resource.ImportStatePassthroughID(ctx, path.Root("mac_address"), req, resp)
}

func (r *staticLeaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data staticLeaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.createLeaseHelper(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Creation Error", err.Error())
		return
	}

	data.ID = data.MacAddress
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *staticLeaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state staticLeaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	log.Printf("[DEBUG] READ START - Target MAC: %s", state.MacAddress.ValueString())
	var scrapedLease struct {
		Hostname string `json:"hostname"`
		IP       string `json:"ip"`
		Enabled  bool   `json:"enabled"`
		Found    bool   `json:"found"`
	}

	// IP is in column 1, MAC is in column 2, Hostname in 3
	jsExpression := `
    (function(targetMac) {
    const table = document.getElementById('BR.1.LEASES.STATIC');
    if (!table) return { found: false };

    const rows = Array.from(table.querySelectorAll('tr'));
    console.log("timing issue check. rows: " + rows.length);
    for (const row of rows) {
        // We look for the cell containing the MAC (Cell index 1)
        const macCell = row.cells[1];
        if (macCell && macCell.innerText.toLowerCase().trim() === targetMac.toLowerCase()) {
            const enabledCheckbox = row.cells[3].querySelector('input[type="checkbox"]');
            return {
                ip:       row.cells[0].innerText.trim(),
                hostname: row.cells[2].innerText.trim(),
                enabled:  enabledCheckbox ? enabledCheckbox.checked : false,
                found:    true
            };
        }
    }
    return { found: false };
})("` + state.MacAddress.ValueString() + `")`

	actions := append(r.navigateStaticLeaseActions(),
		chromedp.Evaluate(jsExpression, &scrapedLease),
	)

	err := r.client.RunActions(ctx, actions...)
	log.Printf("[DEBUG] READ RESULT - Found: %t, Host: %s, IP: %s",
		scrapedLease.Found, scrapedLease.Hostname, scrapedLease.IP)

	if err != nil || !scrapedLease.Found {
		log.Printf("[WARN] Resource not found on router, removing from state")
		resp.State.RemoveResource(ctx)
		return
	}

	// Update state with what we actually found on the router
	state.Hostname = types.StringValue(scrapedLease.Hostname)
	state.IPAddress = types.StringValue(scrapedLease.IP)
	state.Enabled = types.BoolValue(scrapedLease.Enabled)
	state.MacAddress = types.StringValue(strings.ToLower(state.MacAddress.ValueString()))
	state.ID = state.MacAddress
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *staticLeaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state staticLeaseResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.deleteLeaseHelper(ctx, state.MacAddress.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Update Error: Delete Phase", err.Error())
		return
	}

	err = r.createLeaseHelper(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Update Error: Create Phase", err.Error())
		return
	}

	plan.ID = plan.MacAddress
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *staticLeaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data staticLeaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.deleteLeaseHelper(ctx, data.MacAddress.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Delete Error", err.Error())
		return
	}
}

func (r *staticLeaseResource) deleteLeaseHelper(ctx context.Context, mac string) error {
	var EntryFound bool
	var overlayText string

	log.Printf("[DEBUG] deleting lease of %s.", mac)
	jsClickRemove := `
        (function(targetMac) {
            const table = document.getElementById('BR.1.LEASES.STATIC');
            const rows = Array.from(table.querySelectorAll('tr'));
            for (const row of rows) {
                const macCell = row.cells[1];
                if (macCell && macCell.innerText.toLowerCase().trim() === targetMac.toLowerCase()) {
                    const removeBtn = row.querySelector('input[value="Remove"]');
                    if (removeBtn) { 
                        removeBtn.dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true}));
                        return true; 
                    }
                }
            }
            return false;
        })("` + mac + `")`

	actions := append(r.navigateStaticLeaseActions(),
		chromedp.Evaluate(jsClickRemove, &EntryFound),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.ActionFunc(func(_ context.Context) error {
			if EntryFound {
				log.Printf("[DEBUG] Entry found for %s.", mac)
				return nil
			}
			return fmt.Errorf("Existing lease not found")
		}),
		chromedp.WaitVisible(`#btn_apply`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Click(`#btn_apply`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] Clicked apply.")
			return nil
		}),
		chromedp.WaitVisible(`#content_overlay_panel`, chromedp.ByID),
		chromedp.Text(`#content_overlay_content`, &overlayText, chromedp.ByID),
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] Dialogue message: %s.", overlayText)
			return nil
		}),
		chromedp.WaitVisible(`.C_CSS_flatbtn[value="Continue"]`, chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.WaitNotVisible(`.C_CSS_LoadingDiv`, chromedp.ByQuery),
		chromedp.Click(`.C_CSS_flatbtn[value="Continue"]`, chromedp.ByQuery),
		chromedp.WaitNotVisible(`#content_overlay`, chromedp.ByID),
		chromedp.WaitReady(`#BR\.1\.LEASES\.STATIC`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
	)

	return r.client.RunActions(ctx, actions...)
}

func (r *staticLeaseResource) createLeaseHelper(ctx context.Context, data staticLeaseResourceModel) error {
	var overlayText string
	var hasErrorBox bool

	actions := append(r.navigateStaticLeaseActions(),
		chromedp.SetValue(`#HLP\.action\.newstaticlease\.ip`, data.IPAddress.ValueString(), chromedp.ByID),
		chromedp.SetValue(`#HLP\.action\.newstaticlease\.mac`, data.MacAddress.ValueString(), chromedp.ByID),
		chromedp.SetValue(`#HLP\.action\.newstaticlease\.host`, data.Hostname.ValueString(), chromedp.ByID),
	)

	if !data.Enabled.IsNull() && data.Enabled.ValueBool() {
		actions = append(actions, chromedp.SetAttributeValue(`#HLP\.action\.newstaticlease\.status`, "checked", "true", chromedp.ByID))
	} else {
		actions = append(actions, chromedp.RemoveAttribute(`#HLP\.action\.newstaticlease\.status`, "checked", chromedp.ByID))
	}

	actions = append(actions,
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] about to click Add")
			return nil
		}),

		chromedp.Click(`tr#BRIDGE\.1\.STATICLEASES\.0\.INPUT input[value="Add"]`, chromedp.ByQuery),

		// Router may refuse the entry via an alert box if there's an existing entry
		chromedp.Sleep(500*time.Millisecond),
		chromedp.ActionFunc(func(_ context.Context) error {
			if r.client.AlertFound {
				log.Printf("router alert: %s", r.client.AlertMsg)
				return fmt.Errorf("halting: router alert: %s", r.client.AlertMsg)
			}
			return nil
		}),

		chromedp.WaitVisible(`#btn_apply`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] about to click Apply")
			return nil
		}),

		chromedp.Click(`#btn_apply`, chromedp.ByID),

		chromedp.WaitVisible(`#content_overlay_panel`, chromedp.ByID),
		chromedp.Evaluate(`document.querySelector('.C_CSS_MsgReportBox') !== null`, &hasErrorBox),
		chromedp.Text(`#content_overlay_content`, &overlayText, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),

		// Router may refuse the config if it doesn't like the IP address
		chromedp.ActionFunc(func(ctx context.Context) error {
			log.Printf("[DEBUG] Dialogue message: %s.", overlayText)

			if hasErrorBox {
				log.Printf("[ERROR] Router reject config: %s. Starting cleanup.", overlayText)
				if err := chromedp.Click(`.C_CSS_flatbtn[value="Continue"]`, chromedp.ByQuery).Do(ctx); err != nil {
					return err
				}

				if err := chromedp.WaitVisible(`#BR\.1\.LEASES\.STATIC`, chromedp.ByID).Do(ctx); err != nil {
					return err
				}
				if err := chromedp.Sleep(500 * time.Millisecond).Do(ctx); err != nil {
					return err
				}

				jsCleanup :=
					`(function(targetMac) {
                                    const table = document.getElementById('BR.1.LEASES.STATIC');
                                    const rows = Array.from(table.querySelectorAll('tr'));
                                    for (const row of rows) {
                                        const macCell = row.cells[1];
                                        if (macCell && macCell.innerText.toLowerCase().trim() === targetMac.toLowerCase()) {
                                            const removeBtn = row.querySelector('input[value="Remove"]');
                                            if (removeBtn) { 
                                                removeBtn.dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true}));
                                                return true; 
                                            }
                                        }
                                    }
                                    return false;
                                })("` + data.MacAddress.ValueString() + `")`

				var removed bool
				if err := chromedp.Evaluate(jsCleanup, &removed).Do(ctx); err != nil {
					return err
				}

				if removed {
					log.Printf("[DEBUG] Removing stale entry for Target MAC: %s",
						data.MacAddress.ValueString())

				} else {
					log.Printf("[WARN] No match found for MAC %s in the table.", data.MacAddress.ValueString())
				}
				// 4. If we successfully clicked remove, we MUST hit apply again to persist the cleanup
				if removed {
					if err := chromedp.Click(`#btn_apply`, chromedp.ByID).Do(ctx); err != nil {
						return err
					}

					if err := chromedp.WaitVisible(`#content_overlay_panel`, chromedp.ByID).Do(ctx); err != nil {
						return err
					}

				}
			}
			log.Printf("[DEBUG] Dismissing final overlay dialogue (triggering the service update)")
			if err := chromedp.Run(ctx,
				chromedp.WaitVisible(`.C_CSS_flatbtn[value="Continue"]`, chromedp.ByQuery),
				chromedp.Sleep(200*time.Millisecond),
				chromedp.WaitNotVisible(`.C_CSS_LoadingDiv`, chromedp.ByQuery),
				chromedp.Click(`.C_CSS_flatbtn[value="Continue"]`, chromedp.ByQuery)); err != nil {
				return err
			}

			log.Printf("[DEBUG] Waiting for page to reload")
			if err := chromedp.WaitNotVisible(`#content_overlay`, chromedp.ByID).Do(ctx); err != nil {
				return err
			}
			return chromedp.Sleep(300 * time.Millisecond).Do(ctx)
		}),
	)

	err := r.client.RunActions(ctx, actions...)
	if err != nil {
		return fmt.Errorf("browser session failed: %w", err)
	}

	if hasErrorBox {
		return fmt.Errorf("router rejected configuration: %s", overlayText)
	}

	log.Printf("[INFO] Router reported success: %s", overlayText)
	return nil
}

func (r *staticLeaseResource) navigateStaticLeaseActions() []chromedp.Action {
	return []chromedp.Action{
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] Navigating to static lease page")
			return nil
		}),

		chromedp.Click(`#TREENODE_0`, chromedp.ByID),
		chromedp.WaitVisible(`li[data-nodeid="systemstatus.lan"] > a`, chromedp.ByQuery),
		chromedp.Click(`li[data-nodeid="systemstatus.lan"] > a`, chromedp.ByQuery),
		chromedp.Sleep(200 * time.Millisecond),
		chromedp.WaitVisible(`#BR\.1\.LEASES\.STATIC`, chromedp.ByID),
		chromedp.WaitVisible(`#BRIDGE\.1\.STATICLEASES\.0\.INPUT`, chromedp.ByID),
		// Standardize the wait for data to populate
		chromedp.Sleep(500 * time.Millisecond),
		chromedp.WaitVisible(`input[value="Add"]`, chromedp.ByQuery),
		chromedp.Sleep(500 * time.Millisecond),
	}
}
