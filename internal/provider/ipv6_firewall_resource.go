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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"log"
	"strconv"
	"strings"
	"time"
)

var (
	_ resource.Resource                = &iPv6FirewallResource{}
	_ resource.ResourceWithConfigure   = &iPv6FirewallResource{}
	_ resource.ResourceWithImportState = &iPv6FirewallResource{}
	_ resource.ResourceWithModifyPlan  = &iPv6FirewallResource{}
)

type iPv6FirewallResourceModel struct {
	ID                      types.String `tfsdk:"id"` // 1-32
	Name                    types.String `tfsdk:"name"`
	Protocol                types.String `tfsdk:"protocol"`
	PortStart               types.Int64  `tfsdk:"port_start"`
	PortEnd                 types.Int64  `tfsdk:"port_end"`
	SourceIP6               types.String `tfsdk:"source_ip"`
	SourcePrefixLength      types.Int64  `tfsdk:"source_prefix_length"`
	DestinationIP6          types.String `tfsdk:"destination_ip"`
	DestinationPrefixLength types.Int64  `tfsdk:"destination_prefix_length"`
}

type iPv6FirewallResource struct {
	client *IcoteraClient
}

// Metadata returns the resource type name.
func (r *iPv6FirewallResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ipv6_firewall"
}

// Schema defines the schema for the resource.
func (r *iPv6FirewallResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `IPv6 Firewall

This resource setups up IPv6 firewall rules that open inbound traffic on certain ports to certain internal destinations. Inbound traffic can be restricted from certain source ip addresses.

The provider adds entries to the bottom of the firewall rule list in the router web interface, which visually separates automatic entries from any manually administered entries at the top of the page. The provider will not overwrite any enabled manual entries.`,

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The 1-32 slot index on the router.",
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
				Description: "tcp, udp, or any",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("tcp", "udp", "any"),
				},
			},
			"port_start": schema.Int64Attribute{
				Description: "The lowest (or only) port in the range of ports to be opened",
				Required:    true,
				Validators:  []validator.Int64{int64validator.Between(1, 65535)},
			},
			"port_end": schema.Int64Attribute{
				Description: "The highest port of the port range to be opened (can be left out when opening single ports)",
				Required:    false,
				Optional:    true,
				Computed:    true,
				Validators:  []validator.Int64{int64validator.Between(1, 65535)},
			},
			"source_ip": schema.StringAttribute{
				Description: "The source IPv6 address(es) that this rule applies to (use :: for all)",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("::"),
			},
			"source_prefix_length": schema.Int64Attribute{
				Description: "The IPv6 prefix length applied to the source IPv6 address",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(0),
				Validators:  []validator.Int64{int64validator.Between(0, 128)},
			},
			"destination_ip": schema.StringAttribute{
				Description: "The destination IPv6 address(es) that this rule applies to",
				Required:    true,
			},

			"destination_prefix_length": schema.Int64Attribute{
				Description: "The IPv6 prefix length applied to the destination IPv6 address",
				Required:    true,
				Validators:  []validator.Int64{int64validator.Between(0, 128)},
			},
		},
	}
}

// Configure adds the provider-level client to the resource.
func (r *iPv6FirewallResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *iPv6FirewallResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		iPv6FirewallConfigValidator{},
	}
}

type iPv6FirewallConfigValidator struct{}

func (v iPv6FirewallConfigValidator) Description(_ context.Context) string {
	return "validates that loopback only uses a single port"
}

func (v iPv6FirewallConfigValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v iPv6FirewallConfigValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data iPv6FirewallResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !data.PortEnd.IsNull() {
		start := data.PortStart.ValueInt64()
		end := data.PortEnd.ValueInt64()

		if end != 0 && end < start {
			resp.Diagnostics.AddAttributeError(
				path.Root("port_end"),
				"Invalid Port Range",
				"The end port must be unset or greater than or equal to the start port",
			)
		}
	}

	if !data.SourceIP6.IsUnknown() && !data.SourcePrefixLength.IsUnknown() {
		SourceIPIsNULL := data.SourceIP6.IsNull()
		sourcePrefixIsNull := data.SourcePrefixLength.IsNull()

		if SourceIPIsNULL != sourcePrefixIsNull {
			resp.Diagnostics.AddError(
				"Invalid Source Configuration",
				"Both 'source_ip' and 'source_prefix_length' must be provided together, or both must be omitted.",
			)
		}
	}
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *iPv6FirewallResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan iPv6FirewallResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	var applyResult struct {
		IsError bool   `json:"isError"`
		Message string `json:"message"`
	}

	id := plan.ID.ValueString()

	actions := append(
		r.navigateIPv6FirewallActions(),
		r.setRowValues(id, plan)...)

	actions = append(actions, applyAndCheckErrorsActions(r.client, &applyResult)...)

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
func (r *iPv6FirewallResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *iPv6FirewallResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state iPv6FirewallResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	var dipPrefix string
	var dipPrefix2 string
	var scraped struct {
		Name                    string `json:"name"`
		Protocol                string `json:"protocol"`
		PortStart               int64  `json:"port_start"`
		PortEnd                 int64  `json:"port_end"`
		SourceIP6               string `json:"source_ip"`
		SourcePrefixLength      int64  `json:"source_prefix_length"`
		DestinationIP6          string `json:"destination_ip"`
		DestinationPrefixLength int64  `json:"destination_prefix_length"`
		Enabled                 bool   `json:"enabled"`
		Found                   bool   `json:"found"`
	}

	jsScraper := `
(function(id) {
 var getRawVal = function(suffix) {
        var el = document.getElementById('I-Firewall.X_GETOUI_Rules.' + id + '.' + suffix);
        if (!el) return "";
        try {
            if (window.jQuery) { return jQuery(el).val(); }
        } catch (e) {}
        return el.value || "";
    };

    var getInt = function(suffix) {
        var val = getRawVal(suffix);
        if (!val || val.trim() === "") return 0;
        var parsed = parseInt(val, 10);
        return isNaN(parsed) ? 0 : parsed;
    };

    var elEnable = document.getElementById('I-Firewall.X_GETOUI_Rules.' + id + '.Enable');
    if (!elEnable) return { found: false };

    var res = {
        id:                        id,
        name:                      getRawVal('Description'),
        protocol:                  getRawVal('Proto'),
        port_start:                getInt('DPort'),
        port_end:                  getInt('DPortEndRange'),
        source_ip:                 getRawVal('SIP'),
        source_prefix_length:      getInt('SIP_PrefixLength'),
        destination_ip:            getRawVal('DIP'),
        destination_prefix_length: getInt('DIP_PrefixLength'),
        enabled:                   document.getElementById('I-Firewall.X_GETOUI_Rules.' + id + '.Enable').checked,
        found:                     true
    };
    return res;
})("` + id + `")`

	actions := append(r.navigateIPv6FirewallActions(),
		chromedp.Evaluate(jsScraper, &scraped),
		chromedp.Evaluate(fmt.Sprintf(`(window.jQuery && jQuery('#I-Firewall\\.X_GETOUI_Rules\\.%s\\.DIP_PrefixLength').val()) || ""`, id), &dipPrefix),
		chromedp.Value(`#I-Firewall\.X_GETOUI_Rules\.`+id+`\.DIP_PrefixLength`, &dipPrefix2, chromedp.ByID),
	)

	err := r.client.RunActions(ctx, actions...)
	log.Printf("[DEBUG] Scraped for ID %s: %+v", state.ID.ValueString(), scraped)

	if err != nil || !scraped.Found || !scraped.Enabled {
		log.Printf("Resource %s not found/enabled", state.ID.ValueString())
		resp.State.RemoveResource(ctx)
		return
	}

	log.Printf("[DEBUG] Native chromedp read for DIP_PrefixLength: %s", dipPrefix)
	log.Printf("[DEBUG] Native chromedp read for DIP_PrefixLength: %s", dipPrefix2)
	if val, err := strconv.ParseInt(dipPrefix, 10, 64); err == nil {
		scraped.DestinationPrefixLength = val
	}

	configProto := state.Protocol.ValueString()
	if strings.EqualFold(scraped.Protocol, configProto) {
		state.Protocol = types.StringValue(configProto) // Use user's casing
	} else {
		state.Protocol = types.StringValue(scraped.Protocol)
	}
	state.Name = types.StringValue(strings.ToLower(scraped.Name))
	state.Protocol = types.StringValue(strings.ToLower(scraped.Protocol))
	state.PortStart = types.Int64Value(scraped.PortStart)
	state.PortEnd = types.Int64Value(scraped.PortEnd)
	state.SourceIP6 = types.StringValue(scraped.SourceIP6)
	state.SourcePrefixLength = types.Int64Value(scraped.SourcePrefixLength)
	state.DestinationIP6 = types.StringValue(scraped.DestinationIP6)
	state.DestinationPrefixLength = types.Int64Value(scraped.DestinationPrefixLength)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Create creates the resource and sets the initial Terraform state.
func (r *iPv6FirewallResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data iPv6FirewallResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	var discoveredID string

	var applyResult struct {
		IsError bool   `json:"isError"`
		Message string `json:"message"`
	}

	actions := append(r.navigateIPv6FirewallActions(),
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] About to search for free slot.")
			return nil
		}),
		chromedp.Evaluate(`(function() {
		for (let i = 32; i >= 1; i--) {
                        const checkbox = document.getElementById('I-Firewall.X_GETOUI_Rules.' + i + '.Enable');
                        if (checkbox && !checkbox.checked) {
                            return i.toString();
        		}
                }
		return "";
		})()`, &discoveredID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if discoveredID == "" {
				return fmt.Errorf("No available IPv6 firewall slots found (max 32)")
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
	actions = append(actions, applyAndCheckErrorsActions(r.client, &applyResult)...)

	err := r.client.RunActions(ctx, actions...)
	if err != nil {
		resp.Diagnostics.AddError("IPv6 Firewall slot Discovery/Creation Problem", err.Error())
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
func (r *iPv6FirewallResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data iPv6FirewallResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	id := data.ID.ValueString()
	var applyResult struct {
		IsError bool   `json:"isError"`
		Message string `json:"message"`
	}
	actions := append(r.navigateIPv6FirewallActions(),
		// there doesn't seem to be a way to completely clear out an entry you no longer want.
		// You can disable it but this doesn't seem to reset the relevant fields.
		// Setting ports or name to empty strings leads to a validation error.
		// It would be desirable to allow entries to be configured but disabled,
		// as we could then distinguish between entries a user had manually disabled,
		// and entries that had been deleted in terraform.
		// The best alternative is to stick terraform entries at the end of the list
		// so that terraform will not immediately replace entries at the top of the
		// list that the user might have disabled.

		r.syncCheckbox(fmt.Sprintf("I-Firewall.X_GETOUI_Rules.%s.Enable", id), false),
		chromedp.Sleep(200*time.Millisecond),
	)

	actions = append(actions, applyAndCheckErrorsActions(r.client, &applyResult)...)

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
func (r *iPv6FirewallResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // Deletion
	}

	var plan iPv6FirewallResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	// Normalization: Force protocol to lowercase
	if !plan.Protocol.IsNull() && !plan.Protocol.IsUnknown() {
		normalizedProtocol := strings.ToLower(plan.Protocol.ValueString())
		plan.Protocol = types.StringValue(normalizedProtocol)
	}

	if !plan.PortStart.IsUnknown() && (plan.PortEnd.IsUnknown() || plan.PortEnd.IsNull()) {
		plan.PortEnd = plan.PortStart
	}

	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

/* this always ensures the row is enabled in order to make it editable */
func (r *iPv6FirewallResource) setRowValues(id string, plan iPv6FirewallResourceModel) []chromedp.Action {
	actions := []chromedp.Action{
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] setRowValues: %s", id)
			return nil
		}),

		// "Use firewall exceptions" checkbox must be on to do anything at all on this page
		r.syncCheckbox("I-Firewall.Enable", true),
		// row checkbox must be enabled to edit a rule
		r.syncCheckbox(fmt.Sprintf("I-Firewall.X_GETOUI_Rules.%s.Enable", id), true),
		chromedp.Poll(fmt.Sprintf(`!document.querySelector('[id="I-Firewall.X_GETOUI_Rules.%s.Description"]').disabled`, id), nil),
		chromedp.SetValue(fmt.Sprintf("#I-Firewall\\.X_GETOUI_Rules\\.%s\\.Description", id), plan.Name.ValueString()),
		chromedp.SetValue(fmt.Sprintf("#I-Firewall\\.X_GETOUI_Rules\\.%s\\.Proto", id), strings.ToUpper(plan.Protocol.ValueString())),
		chromedp.SetValue(fmt.Sprintf("#I-Firewall\\.X_GETOUI_Rules\\.%s\\.DPort", id), strconv.FormatInt(plan.PortStart.ValueInt64(), 10)),
	}
	if plan.PortEnd.IsNull() {
		actions = append(actions,
			chromedp.ActionFunc(func(_ context.Context) error {
				log.Printf("[DEBUG] setRowValues external port end is null")
				return nil
			}),

			chromedp.SetValue(fmt.Sprintf("#I-Firewall\\.X_GETOUI_Rules\\.%s\\.DPortEndRange", id), strconv.FormatInt(plan.PortStart.ValueInt64(), 10)),
		)
	} else {
		actions = append(actions,
			chromedp.ActionFunc(func(_ context.Context) error {
				log.Printf("[DEBUG] setRowValues external port end is %d", plan.PortEnd.ValueInt64())
				return nil
			}),

			chromedp.SetValue(fmt.Sprintf("#I-Firewall\\.X_GETOUI_Rules\\.%s\\.DPortEndRange", id), strconv.FormatInt(plan.PortEnd.ValueInt64(), 10)),
		)
	}
	actions = append(actions,
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] setRowValues setting source ip to %s, prefix length to %s", strings.ToUpper(plan.SourceIP6.ValueString()), strconv.FormatInt(plan.SourcePrefixLength.ValueInt64(), 10))
			return nil
		}),

		chromedp.SetValue(fmt.Sprintf("#I-Firewall\\.X_GETOUI_Rules\\.%s\\.SIP", id), strings.ToUpper(plan.SourceIP6.ValueString())),
		chromedp.SetValue(fmt.Sprintf("#I-Firewall\\.X_GETOUI_Rules\\.%s\\.SIP_PrefixLength", id), strconv.FormatInt(plan.SourcePrefixLength.ValueInt64(), 10)),

		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] setRowValues setting dest ip to %s, prefix length to %s", strings.ToUpper(plan.DestinationIP6.ValueString()), strconv.FormatInt(plan.DestinationPrefixLength.ValueInt64(), 10))
			return nil
		}),

		chromedp.SetValue(fmt.Sprintf("#I-Firewall\\.X_GETOUI_Rules\\.%s\\.DIP", id), strings.ToUpper(plan.DestinationIP6.ValueString())),
		chromedp.SetValue(fmt.Sprintf("#I-Firewall\\.X_GETOUI_Rules\\.%s\\.DIP_PrefixLength", id), strconv.FormatInt(plan.DestinationPrefixLength.ValueInt64(), 10)),
	)
	return actions
}

func (r *iPv6FirewallResource) syncCheckbox(elementID string, desired bool) chromedp.Action {
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

func (r *iPv6FirewallResource) navigateIPv6FirewallActions() []chromedp.Action {
	return []chromedp.Action{
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] Navigating to ipv6 firewall")
			return nil
		}),
		chromedp.WaitVisible(`#TREENODE_4`, chromedp.ByID),
		chromedp.WaitVisible(`#listcont_TREENODE_4_0`, chromedp.ByID),
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] About to click to ipv6 firewall")
			return nil
		}),
		chromedp.Click(`//ul[@id="sublist_TREENODE_4"]//a[contains(text(), "IPv6 firewall")]`, chromedp.BySearch),
		chromedp.ActionFunc(func(_ context.Context) error {
			log.Printf("[DEBUG] waiting for ipv6 firewall data to load")
			return nil
		}),

		chromedp.WaitVisible(`#firewall`, chromedp.ByID),
		chromedp.Sleep(400 * time.Millisecond),
		// seems to be a two phase loading. First a spinner as an empty page loads.
		// The fields seem to be disabled initially. There is a shorter delay while
		// data loads into the page
		chromedp.Poll(`!document.getElementById('I-Firewall.Enable').disabled`, nil),
	}
}
