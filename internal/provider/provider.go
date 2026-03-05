package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type icoteraProvider struct {
	version string
}

type icoteraProviderModel struct {
	RouterAddress types.String `tfsdk:"router_address"`
	Username      types.String `tfsdk:"username"`
	Password      types.String `tfsdk:"password"`
}

var _ provider.Provider = &icoteraProvider{}

// New returns a function that initializes a new provider instance.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &icoteraProvider{
			version: version,
		}
	}
}

func (p *icoteraProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "icotera-i4850"
	resp.Version = p.version
}

func (p *icoteraProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

func (p *icoteraProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `The provider is used to control the Icotera i4850 domestic router, applying static DHCP leases, IPv4 port forwards and IPv6 firewall rules.

It has been developed against the i4850-31 model but is expected to work with other variants.`,
		Attributes: map[string]schema.Attribute{
			"router_address": schema.StringAttribute{
				Description: "The IPv4 address of the router.",
				Required:    true,
			},
			"username": schema.StringAttribute{
				Description: "The username to log into the router. Defaults to 'admin'.",
				Required:    true,
			},
			"password": schema.StringAttribute{
				Description: "The password used to log into the router.",
				Required:    true,
				Sensitive:   true,
			},
		},
	}
}

func (p *icoteraProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config icoteraProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client, err := NewIcoteraClient(
		config.RouterAddress.ValueString(),
		config.Username.ValueString(),
		config.Password.ValueString(),
	)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", err.Error())
		return
	}

	// Pass the client to all Resources
	resp.ResourceData = client
}

func (p *icoteraProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		func() resource.Resource { return &staticLeaseResource{} },
		func() resource.Resource { return &portForwardResource{} },
	}
}
