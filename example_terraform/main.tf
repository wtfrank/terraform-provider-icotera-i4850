resource "icotera_i4850_static_lease" "example" {
  hostname    = "printer"
  mac_address = "00:11:22:33:44:55"
  ip_address  = "172.16.50.50"
  enabled     = "true"
}
