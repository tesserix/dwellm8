// Package notify is one of the eight modules named in ADR-0001.
//
// It owns its tables and is the only writer of them. Other modules reach it
// through the interface in service/, never through store/ and never through
// its tables — the CI import check enforces that, and the per-module database
// role enforces it again in PostgreSQL.
//
//	http/     handlers, request and response types
//	service/  this module's public Go interface — the extraction seam
//	domain/   aggregates and rules, no framework imports
//	store/    SQL for this module's tables only
//	events/   what it publishes and what it subscribes to
package notify
