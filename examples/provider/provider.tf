terraform {
  required_providers {
    icotera-i4850 = {
      source  = "registry.terraform.io/francis-fisher/icotera-i4850"
    }
  }
}

provider "icotera-i4850" {
  router_address = "192.168.1.1"
  # optional - defaults to "admin"
  username = "admin"
  password = "password"
}
