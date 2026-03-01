A terraform provider for an Icotera i4850 series routers.

Developed with Icotera i4850-31 but expected to work with other variants. Reference https://www.normann-engineering.com/en/products/1445/icotera-i4850/

# FEATURES

* assign static dhcp4 leases (Status-\>LAN)

# PLANNED FEATURES

* configure ip4 port forwarding (Services-\>Port Forwarding)
* configure ip6 firewall (Services-\>Ipv6 firewall)

# INSTALL

The provider uses a headless chrome client (via chromedp) to interact with the router's web interface.

Tested with chromium on ubuntu 24.04
