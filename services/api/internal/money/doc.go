// Package money is one of the eight modules named in ADR-0001.
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
//
// Three siblings sit beside that template rather than inside domain/, because
// each is a genuinely different thing and the collisions were the package
// boundary pointing at itself:
//
//	domain/          the ledger — an event becoming balanced postings (ADR-0006)
//	domain/collect/  a collection with a lifecycle (ADR-0011)
//	recon/           a provider's account of what it settled (ADR-0012)
//	provider/        the seam. No package above this line names an aggregator
package money
