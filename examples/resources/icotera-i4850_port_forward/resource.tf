resource "icotera-i4850_port_forward" "example1" {
  name = "example1"
  # protocol can be "tcp", "udp", or "Both"
  protocol = "udp"
  external_port_start = "5154"
  # You don't need to specify external_port_end if you only want to forward a single port
  # If forwarding a range of external ports, they are all sent to a single internal port
  external_port_end = "5154" 
  internal_ip = "192.168.15.16"
  internal_port = "5155"
  loopback = false
}
