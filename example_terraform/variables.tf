variable "icotera_username" {
  description = "Username for the web front end"
  type        = string
  sensitive   = true
  default     = "admin"
}

variable "icotera_password" {
  description = "Password for the web front end"
  type        = string
  sensitive   = true
}

variable "icotera_address" {
  description = "ipv4 address for the icotera router"
  type        = string
}
