terraform {
  required_providers {
    icotera = {
      source  = "hashicorp.com/wtfrank/icotera-i4850"
    }
  }
}

provider "icotera" {
  endpoint = var.icotera_address
  username = var.icotera_username
  password = var.icotera_password
}
