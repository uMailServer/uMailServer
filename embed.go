package umailserver

import "embed"

//go:embed webmail/dist/*
var WebmailFS embed.FS

//go:embed web/admin/dist/*
var AdminFS embed.FS

//go:embed web/account/dist/*
var AccountFS embed.FS
