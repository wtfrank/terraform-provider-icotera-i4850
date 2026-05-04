package provider

import (
	"context"
	"fmt"
	"github.com/chromedp/chromedp"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"strconv"
	"time"
)

var (
	_ resource.Resource                = &lanSettingsResource{}
	_ resource.ResourceWithConfigure   = &lanSettingsResource{}
	_ resource.ResourceWithImportState = &lanSettingsResource{}
	_ resource.ResourceWithModifyPlan  = &lanSettingsResource{}
)

type lanSettingsResourceModel struct {
	ID            types.String `tfsdk:"id"`
	RouterIP      types.String `tfsdk:"router_ip"`
	SubnetMask    types.String `tfsdk:"subnet_mask"`
	DHCPEnabled   types.Bool   `tfsdk:"dhcp_enabled"`
	PoolStart     types.String `tfsdk:"pool_start"`
	PoolEnd       types.String `tfsdk:"pool_end"`
	LeaseTime     types.Int64  `tfsdk:"lease_time"`
	MaxLeaseTime  types.Int64  `tfsdk:"max_lease_time"`
	PrimaryDNS    types.String `tfsdk:"primary_dns"`
	SecondaryDNS  types.String `tfsdk:"secondary_dns"`
	WINSServer    types.String `tfsdk:"wins_server"`
	Gateway       types.String `tfsdk:"gateway"`
	IPv6RAEnabled types.Bool   `tfsdk:"ipv6_ra_enabled"`
}

type lanSettingsResource struct {
	client *IcoteraClient
}

func (r *lanSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lan_settings"
}

func (r *lanSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages LAN and DHCP settings on the Icotera i4850 router.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Internal ID",
				Computed:    true,
				Default:     stringdefault.StaticString("lan.settings"),
			},

			"router_ip": schema.StringAttribute{
				Description: "The IPv4 address of the router on the LAN. Changing this via Terraform is not supported as it would break the connection.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"subnet_mask": schema.StringAttribute{
				Description: "The subnet mask for the LAN. Changing this via Terraform is not supported.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dhcp_enabled": schema.BoolAttribute{
				Description: "Whether the IPv4 DHCP server is enabled.",
				Required:    true,
			},
			"pool_start": schema.StringAttribute{
				Description: "The start of the DHCP IP pool.",
				Required:    true,
			},
			"pool_end": schema.StringAttribute{
				Description: "The end of the DHCP IP pool.",
				Required:    true,
			},
			"lease_time": schema.Int64Attribute{
				Description: "The default DHCP lease time (seconds). Must be between 60 and 86400.",
				Required:    true,
				Validators: []validator.Int64{
					int64validator.Between(60, 86400),
				},
			},
			"max_lease_time": schema.Int64Attribute{
				Description: "The maximum DHCP lease time (seconds). Must be between 60 and 86400.",
				Required:    true,
				Validators: []validator.Int64{
					int64validator.Between(60, 86400),
				},
			},
			"primary_dns": schema.StringAttribute{
				Description: "The primary IPv4 DNS server.",
				Optional:    true,
				Computed:    true,
			},
			"secondary_dns": schema.StringAttribute{
				Description: "The secondary IPv4 DNS server.",
				Optional:    true,
				Computed:    true,
			},
			"wins_server": schema.StringAttribute{
				Description: "The WINS server (for office networks).",
				Optional:    true,
				Computed:    true,
			},
			"gateway": schema.StringAttribute{
				Description: "The gateway address provided by DHCP (usually the same as the router IP)",
				Required:    true,
			},
			"ipv6_ra_enabled": schema.BoolAttribute{
				Description: "Whether IPv6 Router Advertisement is enabled. This allows devices to be assigned globally routable (subject to firewall rules) IPV6 addresses and IPv6 DNS servers. Note that the IPv6 DNS servers cannot be configured in the router - the ISP-provided dns servers will always be propagated. Disabling this setting seems to disable outbound IPv6 routing.",
				Required:    true,
			},
		},
	}
}

func (r *lanSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *lanSettingsResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "lan.settings")...)
}

func (r *lanSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data lanSettingsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.setLANSettings(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Creation Error", err.Error())
		return
	}

	// Scrape the actual values (especially router_ip/subnet_mask) after apply
	err = r.readLANSettings(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Post-Creation Read Error", err.Error())
		return
	}

	data.ID = types.StringValue("lan.settings")
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *lanSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state lanSettingsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.readLANSettings(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Read Error", err.Error())
		return
	}

	state.ID = types.StringValue("lan.settings")
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *lanSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan lanSettingsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.setLANSettings(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Update Error", err.Error())
		return
	}

	// Scrape the actual values after apply
	err = r.readLANSettings(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Post-Update Read Error", err.Error())
		return
	}

	plan.ID = types.StringValue("lan.settings")
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *lanSettingsResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

func (r *lanSettingsResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}

	resp.Plan.SetAttribute(ctx, path.Root("id"), types.StringValue("lan.settings"))
}

func (r *lanSettingsResource) readLANSettings(ctx context.Context, state *lanSettingsResourceModel) error {
	var scraped struct {
		RouterIP      string `json:"router_ip"`
		SubnetMask    string `json:"subnet_mask"`
		DHCPEnabled   string `json:"dhcp_enabled"`
		PoolStart     string `json:"pool_start"`
		PoolEnd       string `json:"pool_end"`
		LeaseTime     int64  `json:"lease_time"`
		MaxLeaseTime  int64  `json:"max_lease_time"`
		PrimaryDNS    string `json:"primary_dns"`
		SecondaryDNS  string `json:"secondary_dns"`
		WINSServer    string `json:"wins_server"`
		Gateway       string `json:"gateway"`
		IPv6RAEnabled string `json:"ipv6_ra_enabled"`
	}

	jsScraper := `
(function() {
    return {
        router_ip:      document.getElementById('I-IP.Interface.9.IPv4Address.2.IPAddress').value,
        subnet_mask:    document.getElementById('I-IP.Interface.9.IPv4Address.2.SubnetMask').value,
        dhcp_enabled:   document.getElementById('I-HLP.bridge.iptype-1').value,
        pool_start:     document.getElementById('I-DHCPv4.Server.Pool.1.MinAddress').value,
        pool_end:       document.getElementById('I-DHCPv4.Server.Pool.1.MaxAddress').value,
        lease_time:     parseInt(document.getElementById('I-DHCPv4.Server.Pool.1.LeaseTime').value),
        max_lease_time: parseInt(document.getElementById('I-DHCPv4.Server.Pool.1.MaxLease').value),
        primary_dns:    document.getElementById('I-HLP.br.dhcpd.dns-1.1').value,
        secondary_dns:  document.getElementById('I-HLP.br.dhcpd.dns-1.2').value,
        wins_server:    document.getElementById('I-DHCPv4.Server.Pool.1.X_GETOUI_Wins').value,
        gateway:        document.getElementById('I-DHCPv4.Server.Pool.1.IPRouters').value,
        ipv6_ra_enabled: document.getElementById('I-RouterAdvertisement.X_GETOUI_BridgeAdvSetting.1.Enable').value
    };
})()`

	actions := append(r.navigateLANActions(),
		chromedp.Evaluate(jsScraper, &scraped),
	)

	err := r.client.RunActions(ctx, actions...)
	if err != nil {
		return err
	}

	state.RouterIP = types.StringValue(scraped.RouterIP)
	state.SubnetMask = types.StringValue(scraped.SubnetMask)
	state.DHCPEnabled = types.BoolValue(scraped.DHCPEnabled == "DHCP server")
	state.PoolStart = types.StringValue(scraped.PoolStart)
	state.PoolEnd = types.StringValue(scraped.PoolEnd)
	state.LeaseTime = types.Int64Value(scraped.LeaseTime)
	state.MaxLeaseTime = types.Int64Value(scraped.MaxLeaseTime)
	state.PrimaryDNS = types.StringValue(scraped.PrimaryDNS)
	state.SecondaryDNS = types.StringValue(scraped.SecondaryDNS)
	state.WINSServer = types.StringValue(scraped.WINSServer)
	state.Gateway = types.StringValue(scraped.Gateway)
	state.IPv6RAEnabled = types.BoolValue(scraped.IPv6RAEnabled == "true")

	return nil
}

func (r *lanSettingsResource) setLANSettings(ctx context.Context, data lanSettingsResourceModel) error {
	var applyResult struct {
		IsError bool   `json:"isError"`
		Message string `json:"message"`
	}

	dhcpType := "Static"
	if data.DHCPEnabled.ValueBool() {
		dhcpType = "DHCP server"
	}

	actions := append(r.navigateLANActions(),
		chromedp.SetValue(`#I-HLP\.bridge\.iptype-1`, dhcpType, chromedp.ByID),
		chromedp.SetValue(`#I-DHCPv4\.Server\.Pool\.1\.MinAddress`, data.PoolStart.ValueString(), chromedp.ByID),
		chromedp.SetValue(`#I-DHCPv4\.Server\.Pool\.1\.MaxAddress`, data.PoolEnd.ValueString(), chromedp.ByID),
		chromedp.SetValue(`#I-DHCPv4\.Server\.Pool\.1\.LeaseTime`, strconv.FormatInt(data.LeaseTime.ValueInt64(), 10), chromedp.ByID),
		chromedp.SetValue(`#I-DHCPv4\.Server\.Pool\.1\.MaxLease`, strconv.FormatInt(data.MaxLeaseTime.ValueInt64(), 10), chromedp.ByID),
		chromedp.SetValue(`#I-HLP\.br\.dhcpd\.dns-1\.1`, data.PrimaryDNS.ValueString(), chromedp.ByID),
		chromedp.SetValue(`#I-HLP\.br\.dhcpd\.dns-1\.2`, data.SecondaryDNS.ValueString(), chromedp.ByID),
		chromedp.SetValue(`#I-DHCPv4\.Server\.Pool\.1\.X_GETOUI_Wins`, data.WINSServer.ValueString(), chromedp.ByID),
		chromedp.SetValue(`#I-DHCPv4\.Server\.Pool\.1\.IPRouters`, data.Gateway.ValueString(), chromedp.ByID),
		chromedp.SetValue(`#I-RouterAdvertisement\.X_GETOUI_BridgeAdvSetting\.1\.Enable`, strconv.FormatBool(data.IPv6RAEnabled.ValueBool()), chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
	)

	actions = append(actions, applyAndCheckErrorsActions(r.client, &applyResult)...)

	err := r.client.RunActions(ctx, actions...)
	if err != nil {
		return err
	}

	if applyResult.IsError {
		return fmt.Errorf("router rejected LAN settings: %s", applyResult.Message)
	}

	return nil
}

func (r *lanSettingsResource) navigateLANActions() []chromedp.Action {
	return []chromedp.Action{
		chromedp.WaitVisible(`#TREENODE_1`, chromedp.ByID),
		chromedp.WaitVisible(`li[data-nodeid="settings.lan"] > a`, chromedp.ByQuery),
		chromedp.Click(`li[data-nodeid="settings.lan"] > a`, chromedp.ByQuery),
		chromedp.WaitVisible(`.C_CSS_LoadingDiv`, chromedp.ByQuery),
		chromedp.WaitNotVisible(`.C_CSS_LoadingDiv`, chromedp.ByQuery),
		chromedp.WaitVisible(`#I-HLP\.bridge\.iptype-1`, chromedp.ByID),
		chromedp.Sleep(100 * time.Millisecond),
	}
}
