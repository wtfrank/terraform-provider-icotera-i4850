package provider

import (
	"context"
	"fmt"
	"github.com/chromedp/chromedp"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"log"
	"strconv"
	"strings"
	"time"
)

var (
	_ resource.Resource                = &portForwardResource{}
	_ resource.ResourceWithConfigure   = &portForwardResource{}
	_ resource.ResourceWithImportState = &portForwardResource{}
	_ resource.ResourceWithModifyPlan  = &portForwardResource{}
)

type portForwardResourceModel struct {
	ID                types.String `tfsdk:"id"` // 1-128
	Name              types.String `tfsdk:"name"`
	Protocol          types.String `tfsdk:"protocol"`
	ExternalPortStart types.Int64  `tfsdk:"external_port_start"`
	ExternalPortEnd   types.Int64  `tfsdk:"external_port_end"`
	InternalIP        types.String `tfsdk:"internal_ip"`
	InternalPort      types.Int64  `tfsdk:"internal_port"`
	Loopback          types.Bool   `tfsdk:"loopback"`
}

type portForwardResource struct {
	client *IcoteraClient
}

// Metadata returns the resource type name.
func (r *portForwardResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_port_forward"
}

// Schema defines the schema for the resource.
func (r *portForwardResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `IPv4 Port Forwarding

This resource sets up port forwarding rules that forward external requests to a particular port (or range of ports) to a single port on a device inside the network.

The provider adds entries to the bottom of the port forward list in the router web interface, which visually separates automatic entries from any manually administered entries at the top of the page. The provider will not overwrite any enabled manual entries.`,

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The 1-128 slot index on the router.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "A name for the forwarding rule",
				Required:    true,
			},
			"protocol": schema.StringAttribute{
				Description: "tcp, udp, or both",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("tcp", "udp", "both"),
				},
			},
			"external_port_start": schema.Int64Attribute{
				Description: "The lowest (or only) port in the external port range",
				Required:    true,
				Validators:  []validator.Int64{int64validator.Between(1, 65535)},
			},
			"external_port_end": schema.Int64Attribute{
				Description: "The highest port of the external port range (can be left out when forwarding single ports)",
				Required:    false,
				Optional:    true,
				Computed:    true,
				Validators:  []validator.Int64{int64validator.Between(1, 65535)},
			},
			"internal_ip": schema.StringAttribute{
				Description: "The internal IPv4 address that incoming connections will be forwarded to",
				Required:    true,
			},
			"internal_port": schema.Int64Attribute{
				Description: "The internal port that external connections will be forwarded to",
				Required:    true,
			},
			"loopback": schema.BoolAttribute{
				Description: "Whether to also forward the port for requests from inside the network (so you could access a service via the external ip address whether inside or outside the network)",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
		},
	}
}

// Configure adds the provider-level client to the resource.
func (r *portForwardResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*IcoteraClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *IcoteraClient, got: %T", req.ProviderData))
		return
	}

	r.client = client
}

// ConfigValidators returns a list of functions that validate the resource configuration.
func (r *portForwardResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		portForwardConfigValidator{},
	}
}

type portForwardConfigValidator struct{}

func (v portForwardConfigValidator) Description(_ context.Context) string {
	return "validates that loopback only uses a single port"
}

func (v portForwardConfigValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v portForwardConfigValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data portForwardResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !data.Loopback.IsNull() && data.Loopback.ValueBool() {
		start := data.ExternalPortStart.ValueInt64()
		end := data.ExternalPortEnd.ValueInt64()

		if end != 0 && end != start {
			resp.Diagnostics.AddAttributeError(
				path.Root("external_port_end"),
				"Invalid Port Range for Loopback",
				"When NAT Loopback is enabled, the external port end must be unset or equal to the start port.",
			)
		}
	}
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *portForwardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan portForwardResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	var applyResult struct {
		IsError bool   `json:"isError"`
		Message string `json:"message"`
	}

	id := plan.ID.ValueString()

	actions := append(
		r.navigatePortForwardActions(),
		r.setRowValues(id, plan)...)

	actions = append(actions, r.applyAndCheckErrorsActions(&applyResult)...)

	if err := r.client.RunActions(ctx, actions...); err != nil {
		resp.Diagnostics.AddError("Update Error", err.Error())
		return
	}

	if applyResult.IsError {
		resp.Diagnostics.AddError(
			"Router Validation Error",
			fmt.Sprintf("The Icotera router rejected the updated settings: %s", applyResult.Message),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// ImportState imports an existing resource into Terraform state (using the row in the port forward table as ID)
func (r *portForwardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *portForwardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state portForwardResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var scraped struct {
		Name              string `json:"name"`
		Protocol          string `json:"protocol"`
		ExternalPortStart int64  `json:"ext_start"`
		ExternalPortEnd   int64  `json:"ext_end"`
		InternalIP        string `json:"internal_ip"`
		InternalPort      int64  `json:"internal_port"`
		Loopback          bool   `json:"loopback"`
		Enabled           bool   `json:"enabled"`
		Found             bool   `json:"found"`
	}

	jsScraper := `
(function(id) {
    return {
        id:            id,
        name:          document.getElementById('I-NAT.PortMapping.' + id + '.Alias').value,
        protocol:      document.getElementById('I-NAT.PortMapping.' + id + '.Protocol').value,
        ext_start:     parseInt(document.getElementById('I-HLP.portfwd.e_extport-' + id + '.1').value),
        ext_end:       parseInt(document.getElementById('I-HLP.portfwd.e_extport-' + id + '.2').value || 0),
        internal_ip:   document.getElementById('I-NAT.PortMapping.' + id + '.InternalClient').value,
        internal_port: parseInt(document.getElementById('I-NAT.PortMapping.' + id + '.InternalPort').value),
        loopback:      document.getElementById('I-NAT.PortMapping.' + id + '.X_GETOUI_Loopback').checked,
        enabled:       document.getElementById('I-NAT.PortMapping.' + id + '.Enable').checked,
        found:         true
    };
})("` + state.ID.ValueString() + `")`

	actions := append(r.navigatePortForwardActions(),
		chromedp.Evaluate(jsScraper, &scraped))

	err := r.client.RunActions(ctx, actions...)

	if err != nil || !scraped.Found || !scraped.Enabled {
		resp.State.RemoveResource(ctx)
		return
	}

	configProto := state.Protocol.ValueString()
	if strings.EqualFold(scraped.Protocol, configProto) {
		state.Protocol = types.StringValue(configProto) // Use user's casing
	} else {
		state.Protocol = types.StringValue(scraped.Protocol)
	}
	state.Name = types.StringValue(strings.ToLower(scraped.Name))
	state.Protocol = types.StringValue(strings.ToLower(scraped.Protocol))
	state.ExternalPortStart = types.Int64Value(scraped.ExternalPortStart)
	state.ExternalPortEnd = types.Int64Value(scraped.ExternalPortEnd)
	state.InternalIP = types.StringValue(scraped.InternalIP)
	state.InternalPort = types.Int64Value(scraped.InternalPort)
	state.Loopback = types.BoolValue(scraped.Loopback)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Create creates the resource and sets the initial Terraform state.
func (r *portForwardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data portForwardResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	var discoveredID string

	var applyResult struct {
		IsError bool   `json:"isError"`
		Message string `json:"message"`
	}

	actions := append(r.navigatePortForwardActions(),
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] About to search for free slot.")
			return nil
		}),
		chromedp.Evaluate(`(function() {
		for (let i = 128; i >= 1; i--) {
                        const checkbox = document.getElementById('I-NAT.PortMapping.' + i + '.Enable');
                        if (checkbox && !checkbox.checked) {
                            return i.toString();
        		}
                }
		return "";
		})()`, &discoveredID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if discoveredID == "" {
				return fmt.Errorf("No available port forwarding slots found (max 128)")
			}
			log.Printf("[DEBUG] Found free slot %s", discoveredID)
			setActions := r.setRowValues(discoveredID, data)
			for _, action := range setActions {
				if err := action.Do(ctx); err != nil {
					return err
				}
			}
			log.Printf("[DEBUG] finished with setActions")
			return nil
		}),
	)
	actions = append(actions, r.applyAndCheckErrorsActions(&applyResult)...)

	err := r.client.RunActions(ctx, actions...)
	if err != nil {
		resp.Diagnostics.AddError("Port Forwarding Slot Discovery/Creation Problem", err.Error())
		return
	}

	if applyResult.IsError {
		resp.Diagnostics.AddError(
			"Router Validation Error",
			fmt.Sprintf("The Icotera router rejected these settings: %s", applyResult.Message),
		)
		return
	}

	data.ID = types.StringValue(discoveredID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete deletes the resource and removes the Terraform state on success
func (r *portForwardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data portForwardResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	id := data.ID.ValueString()
	var applyResult struct {
		IsError bool   `json:"isError"`
		Message string `json:"message"`
	}
	actions := append(r.navigatePortForwardActions(),
		// there doesn't seem to be a way to completely clear out an entry you no longer want.
		// You can disable it but this doesn't seem to reset the relevant fields.
		// Setting ports or name to empty strings leads to a validation error.
		// It would be desirable to allow entries to be configured but disabled,
		// as we could then distinguish between entries a user had manually disabled,
		// and entries that had been deleted in terraform.
		// The best alternative is to stick terraform entries at the end of the list
		// so that terraform will not immediately replace entries at the top of the
		// list that the user might have disabled.

		r.syncCheckbox(fmt.Sprintf("I-NAT.PortMapping.%s.Enable", id), false),
		chromedp.Sleep(200*time.Millisecond),
	)

	actions = append(actions, r.applyAndCheckErrorsActions(&applyResult)...)

	err := r.client.RunActions(ctx, actions...)

	if err != nil {
		resp.Diagnostics.AddError("Delete Error", fmt.Sprintf("Failed to clear slot %s: %s", id, err))
		return
	}
	if applyResult.IsError {
		resp.Diagnostics.AddError("Router Deletion Rejected", applyResult.Message)
		return
	}

}

// ModifyPlan normalises some of the plan fields
func (r *portForwardResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // Deletion
	}

	var plan portForwardResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	// Normalization: Force protocol to lowercase
	if !plan.Protocol.IsNull() && !plan.Protocol.IsUnknown() {
		normalizedProtocol := strings.ToLower(plan.Protocol.ValueString())
		plan.Protocol = types.StringValue(normalizedProtocol)
	}

	if plan.Loopback.IsNull() || plan.Loopback.IsUnknown() {
		plan.Loopback = types.BoolValue(false)
	}

	if !plan.ExternalPortStart.IsUnknown() && (plan.ExternalPortEnd.IsUnknown() || plan.ExternalPortEnd.IsNull()) {
		plan.ExternalPortEnd = plan.ExternalPortStart
	}

	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

/* this always ensures the row is enabled in order to make it editable */
func (r *portForwardResource) setRowValues(id string, plan portForwardResourceModel) []chromedp.Action {
	actions := []chromedp.Action{
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] setRowValues: %s", id)
			return nil
		}),
		r.syncCheckbox(fmt.Sprintf("I-NAT.PortMapping.%s.Enable", id), true),
		chromedp.SetValue(fmt.Sprintf("#I-NAT\\.PortMapping\\.%s\\.Alias", id), plan.Name.ValueString()),
		chromedp.SetValue(fmt.Sprintf("#I-NAT\\.PortMapping\\.%s\\.Protocol", id), strings.ToUpper(plan.Protocol.ValueString())),
		chromedp.SetValue(fmt.Sprintf("#I-HLP\\.portfwd\\.e_extport-%s\\.1", id), strconv.FormatInt(plan.ExternalPortStart.ValueInt64(), 10)),
	}
	if plan.ExternalPortEnd.IsNull() {
		actions = append(actions,
			chromedp.ActionFunc(func(_ context.Context) error {
				log.Printf("[DEBUG] setRowValues external port end is null")
				return nil
			}),

			chromedp.Sleep(3*time.Second),
			chromedp.SetValue(fmt.Sprintf("#I-HLP\\.portfwd\\.e_extport-%s\\.2", id), strconv.FormatInt(plan.ExternalPortStart.ValueInt64(), 10)),
			chromedp.Sleep(3*time.Second),
		)
	} else {
		actions = append(actions,
			chromedp.ActionFunc(func(_ context.Context) error {
				log.Printf("[DEBUG] setRowValues external port end is %d", plan.ExternalPortEnd.ValueInt64())
				return nil
			}),

			chromedp.SetValue(fmt.Sprintf("#I-HLP\\.portfwd\\.e_extport-%s\\.2", id), strconv.FormatInt(plan.ExternalPortEnd.ValueInt64(), 10)))
	}
	actions = append(actions,
		chromedp.SetValue(fmt.Sprintf("#I-NAT\\.PortMapping\\.%s\\.InternalClient", id), plan.InternalIP.ValueString()),
		chromedp.SetValue(fmt.Sprintf("#I-NAT\\.PortMapping\\.%s\\.InternalPort", id), strconv.FormatInt(plan.InternalPort.ValueInt64(), 10)),
		r.syncCheckbox(fmt.Sprintf("I-NAT.PortMapping.%s.X_GETOUI_Loopback", id), plan.Loopback.ValueBool()),
	)
	return actions
}

func (r *portForwardResource) syncCheckbox(elementID string, desired bool) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var isChecked bool
		log.Printf("[DEBUG] Syncing checkbox %s to %t", elementID, desired)
		expr := fmt.Sprintf(`document.getElementById('%s').checked`, elementID)
		if err := chromedp.Evaluate(expr, &isChecked).Do(ctx); err != nil {
			return err
		}
		if isChecked != desired {
			return chromedp.Click("#"+strings.ReplaceAll(elementID, ".", "\\."), chromedp.ByQuery).Do(ctx)
		}
		return nil
	})
}

func (r *portForwardResource) navigatePortForwardActions() []chromedp.Action {
	return []chromedp.Action{
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] Navgating to port forward")
			return nil
		}),
		chromedp.WaitVisible(`#TREENODE_4`, chromedp.ByID),
		chromedp.WaitVisible(`#listcont_TREENODE_4_0`, chromedp.ByID),
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] About to click to port forward")
			return nil
		}),
		chromedp.Click(`//li[@id="listcont_TREENODE_4_0"]//a[contains(text(), "Port Forwarding")]`, chromedp.BySearch),
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] waiting for port forward data to load")
			return nil
		}),

		chromedp.WaitVisible(`#PORTFWD\.DATA_ROW\.1\.`, chromedp.ByID),
		chromedp.Sleep(400 * time.Millisecond),
	}
}

func (r *portForwardResource) applyAndCheckErrorsActions(result *struct {
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
			if r.client.AlertFound {
				log.Printf("router alert: %s", r.client.AlertMsg)
				return fmt.Errorf("halting: router alert: %s", r.client.AlertMsg)
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
		chromedp.Sleep(200 * time.Millisecond),
		chromedp.WaitNotVisible(`.C_CSS_LoadingDiv`, chromedp.ByQuery),
		chromedp.Click(`.C_CSS_flatbtn[value="Continue"]`, chromedp.ByQuery),
		chromedp.WaitNotVisible(`#content_overlay`, chromedp.ByID),
	}
}
