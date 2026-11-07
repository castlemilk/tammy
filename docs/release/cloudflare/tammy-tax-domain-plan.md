# Move tammy.tax to the existing Cloudflare Worker

Status: migration pending. Public DNS and official documentation checked on 7 November 2026 at approximately 04:10 UTC. No zone, DNS, nameserver, domain route, credential or permission changes were made during this assessment.

## Selected route

Use a **free Cloudflare Full DNS setup** for `tammy.tax` in the existing account `2132ccf47ceb5fff234c34d85490470a`, then attach the apex hostname to Worker `tammy-site-direct`. Its existing URL is <https://tammy-site-direct.ben-ebsworth.workers.dev/>.

This changes authoritative DNS by updating nameservers at the current registrar. It does **not** require transferring the domain registration. Cloudflare's partial/CNAME setup, which retains another authoritative DNS provider, requires a Business or Enterprise plan and is outside this plan. See [Full setup](https://developers.cloudflare.com/dns/zone-setups/full-setup/setup/) and [partial setup availability](https://developers.cloudflare.com/dns/zone-setups/partial-setup/).

A read-only check of the signed-in Cloudflare dashboard's complete Domains list showed all nine zones in this account; `tammy.tax` was absent. **No Cloudflare nameserver pair has been assigned to this domain yet.** Do not copy nameservers from another zone or invent a pair.

## Public DNS evidence

Queries included the public authoritative server `ns1.vercel-dns.com`.

| Record | Observation |
| --- | --- |
| NS | `ns1.vercel-dns.com`, `ns2.vercel-dns.com`; TTL 86400 |
| Apex A | `162.159.143.30`, `172.66.3.26`; TTL 60 |
| Apex CAA | `letsencrypt.org`, `pki.goog`, `sectigo.com`; TTL 60 |
| `_openai-site-verification` TXT | Existing verification record present |
| `_cf-custom-hostname` TXT | Existing verification record present |
| Apex MX, TXT and AAAA; `_dmarc` TXT | Authoritative queries returned `NOERROR` with no answer records |
| DS | No DS answer observed in the public query; this is not a registrar-side DNSSEC verification |
| `www` | Resolved Vercel A addresses varied between queries; no CNAME answer was returned |

This is **not a complete zone export**. Public queries cannot enumerate every hostname, custom DKIM selector, verification record, delegation or provider-specific ALIAS configuration. Do not recreate `www` by treating its observed, varying IP addresses as its authoritative provider configuration. Cloudflare also states that its [DNS quick scan can miss records](https://developers.cloudflare.com/dns/zone-setups/reference/dns-quick-scan/).

## Access boundary

The Vercel private DNS connector's list-records operation returned **HTTP 403 permission denied**. That denied operation was not retried, and no equivalent private DNS access was attempted without new authorization.

The currently authorized Wrangler scopes are `account:read`, `workers_scripts:write` and `offline_access`. They do not authorize creating a zone or importing/editing DNS records. Those operations require approved access or an authorized operator. The [Worker domain attachment API](https://developers.cloudflare.com/api/resources/workers/subresources/domains/methods/update/) accepts `Workers Scripts Write`, but attachment still requires an active owned zone. Do not expand permissions or create credentials implicitly.

## Remaining actions

1. Obtain an **authorized complete inventory/export** from the current DNS provider. Review all records, including mail routing, SPF/DKIM/DMARC, verification records, CAA, subdomains, delegations and any flattened ALIAS configuration. Preserve records unrelated to the reviewed website cutover.
2. With approved access, create the **free** `tammy.tax` zone in the existing Cloudflare account and import/review that inventory. Capture the actual assigned nameserver pair. Do not activate a paid plan, transfer the registration or remove existing verification records as part of this step.
3. Confirm and perform the exact nameserver replacement at the current registrar. Recheck DNSSEC/DS state immediately before the change and coordinate any required DNSSEC migration; the public no-DS observation above is insufficient to skip that check. Wait for Cloudflare to report the zone active and verify delegation publicly. Follow the [full-setup migration procedure](https://developers.cloudflare.com/dns/zone-setups/full-setup/setup/).
4. Attach **only `tammy.tax`** as a [Custom Domain](https://developers.cloudflare.com/workers/configuration/routing/custom-domains/) to the existing `tammy-site-direct` Worker. Review any conflicting apex records before replacement. Cloudflare manages the resulting DNS record and certificate. A bare CNAME to `workers.dev` is not this supported attachment flow. Leave `www` and other hostnames unchanged unless separately reviewed.
5. Verify HTTPS, the expected deployed content, representative pages, canonical/sitemap behavior and invoice/quote downloads. Verify preserved non-web records as applicable. Record the zone, nameservers, Worker domain attachment and acceptance evidence before describing `tammy.tax` as migrated.

The DNS cutover and Cloudflare zone access are separate from the GitHub CI credential and Cuttlefish runner gates. A successful local Worker deployment does not establish either DNS migration or unattended CI deployment.
