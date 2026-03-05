resource "icotera-i4850_static_lease" "example" {
  hostname    = "printer"
  mac_address = "00:11:22:33:44:55"
  # ip_address should be within the range configured by address & netmask at Settings->LAN on the router interface
  ip_address  = "192.168.4.4"
  enabled     = "true"
}
