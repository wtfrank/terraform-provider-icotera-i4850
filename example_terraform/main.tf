resource "icotera-i4850_static_lease" "sl_example_1" {
  hostname    = "example_host_1"
  mac_address = "00:11:22:33:44:55"
  ip_address  = "172.16.50.50"
  enabled     = "true"
}

resource "icotera-i4850_static_lease" "sl_example_2" {
  hostname    = "example_host_2"
  mac_address = "11:22:33:44:55:66"
  ip_address  = "172.16.50.51"
  enabled     = "true"
}


# the provider will fill entries from the bottom of the port forward list
# in the router web interface. This will visually separate automatic
# entries from any manually administered ones at the top of the page.
resource "icotera-i4850_port_forward" "pf_example_1" {
  name = "pf_example_1"
  protocol = "udp"
  external_port_start = "5154"
  external_port_end = "5154" # you don't need to specify external_port end if you only want a single port
  internal_ip = "172.16.40.41"
  internal_port = "5155"
  loopback = false
}

resource "icotera-i4850_port_forward" "pf_example_2" {
  name = "pf_example_2"
  protocol = "udp"
  external_port_start = "6154"
  internal_ip = "172.16.40.41"
  internal_port = "6154"
  loopback = false
}


resource "icotera-i4850_ipv6_firewall" "fw_example_1" {
  name = "fw_example_1"
  protocol = "tcp"
  port_start = 555
  port_end = 555

  source_ip = "::"
  source_prefix_length = 128

  destination_ip = "::"
  destination_prefix_length = 0
}

resource "icotera-i4850_ipv6_firewall" "fw_example_2" {
  name = "fw_example_2"
  protocol = "udp"
  port_start = 558

  destination_ip = "::"
  destination_prefix_length = 128
}

resource "icotera-i4850_lan_settings" "example_lan_settings" {
  dhcp_enabled    = true
  pool_start      = "172.16.4.5"
  pool_end        = "172.16.4.250"
  lease_time      = 86400
  max_lease_time  = 86400
  primary_dns     = "1.0.0.1"
  secondary_dns   = "1.1.1.1"
  wins_server     = "0.0.0.0"
  gateway         = "172.16.4.1"
  ipv6_ra_enabled = true
}
