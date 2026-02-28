package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type icoteraProvider struct{}

type icoteraProviderModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
}

var _ provider.Provider = &icoteraProvider{}

func New() provider.Provider {
	return &icoteraProvider{}
}

func (p *icoteraProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "icotera-i4850"
}

func (p *icoteraProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return nil
}

func (p *icoteraProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{Required: true},
			"username": schema.StringAttribute{Required: true},
			"password": schema.StringAttribute{Required: true, Sensitive: true},
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
		config.Endpoint.ValueString(),
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

func (p *icoteraProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		func() resource.Resource { return &StaticLeaseResource{} },
		//        func() resource.Resource { return *DhcpResource{} },
	}
}
