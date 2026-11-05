# Tax integration adapter and specification inventory — 24 October 2026

## What is actually implemented

- `services/sbr-helper/internal/simulator/adapter.go` implements the fixed `SIM.READINESS.0001` synthetic fixture and six closed transport/readiness cases. It is network-disabled and stateless; Core owns durable idempotency and recovery.
- `services/sbr-helper/internal/runner/runner.go` provides the helper component/signer boundaries. `internal/evte/adapter_unavailable.go` has no endpoint/runtime configuration and returns component-unavailable. Signed registration, service scope and component inputs remain required for future EVTE work.
- `proto/tammy/v1/sbr.proto`, `tax.proto`, `bas_preparation.proto`, `company_tax.proto` and `company_return_submission.proto` are **Tammy internal contracts**. Generated company-return client/state definitions do not establish an implemented official provider transport. Current BAS workpapers and manual-handoff exports do not submit to the ATO.
- No checked-in official AS XSD/WSDL/OpenAPI bundle was found at the inspected base `8fde3884`. Existing repository examples are not signed provider approval or conformance artefacts. The new public-package inventory records separately obtained evidence below; it does not activate a provider.

## Official public sources inspected

[ATO Activity Statements catalogue](https://www.sbr.gov.au/digital-service-providers/developer-tools/australian-taxation-office-ato/activity-statements) lists AS **0004.2025**, actions Get/Validate/Submit, with XML via SBR ebMS3/SBR2. It lists package **v1.0** (15 May 2025), contents guide **v1.01** (12 June 2025), business implementation guide **v1.0** (20 November 2025), SBR ebMS3 WIG **v1.7** and SDK guide **v1.4**. These are catalogue facts; only the ZIP bytes/XSD roots below were inspected in this change.

The [public AS package](https://www.sbr.gov.au/sites/default/files/2025-05/ATO_AS.0004_2025_Package_v1.0.zip) was successfully downloaded without login: **534,444 bytes**, SHA256 `4c1cc7145bc2b1202eff1b590a5cb75a9eb40ac35e7f88b6548279e8c68ca385`. It contains Get/Submit structure and validation spreadsheets, the AS message repository XML, C# implementation archive and XML Schema archive. Downloaded code was not executed and the package is not vendored into this repository. [Exact file/hash inventory](tax-specifications-inventory-2026-10-24.json).

The nested XML Schema archive is SHA256 `62053dac1e97fe87862c1d3292793245e1c89d53017efb4d30a4d0bc5021f231`. Four XSDs were parsed for roots, namespaces and imports/includes:

| XSD file | Root | Namespace |
| --- | --- | --- |
| `ato.as.0004.2025.get.01.00.xsd` | ASGetRequest | `http://www.sbr.gov.au/ato/asgetrequest` |
| `ato.as.0004.2025.get.response.01.00.xsd` | ASGetResponse | `http://www.sbr.gov.au/ato/asgetresponse` |
| `ato.as.0004.2025.submit.01.00.xsd` | ASSubmitRequest | `http://www.sbr.gov.au/ato/assubmitrequest` |
| `ato.as.0004.2025.submit.response.01.00.xsd` | ASSubmitResponse | `http://www.sbr.gov.au/ato/assubmitresponse` |

No external XSD imports/includes occur in those four files. This does not establish full business-rule validation, the catalogue's Validate action mapping, AS4 envelopes/receipts, authentication, response-state semantics, service registry applicability or conformance coverage. Spreadsheet rules, C# implementation and all individual message elements were not audited. No official pending/rejected response model is inferred from these root names.

The [ATO common artefacts index](https://www.sbr.gov.au/ato-common-artefacts-and-reference-documents) separately lists the ATO ebMS3 implementation guide **v2.2** (2 November 2023), physical endpoints **v4.5** (15 October 2026), message repository **v3.57** (14 May 2026), and living service registry **v5.83** (22 October 2026). Those catalogue entries are not a verified runtime endpoint configuration; their package bytes were not inspected here.

## Gated material and remaining qualification

The [ATO DSP content map](https://softwaredevelopers.ato.gov.au/DSPcontentmap) distinguishes public SBR artefacts from controlled SBR ShareFile conformance suites, SDKs, SSL certificates and test machine credentials. [Getting started](https://softwaredevelopers.ato.gov.au/getting_started) requires DSP registration, EVTE testing, security evidence, applicable testing/PVT and production whitelisting. No registration, account agreement, login attempt, certificate, machine credential or production endpoint was obtained or used in this work.

The first [local mock](local-sbr-mock.md) is an adapter-conformant synthetic readiness JSON endpoint. Its own OpenAPI document is deliberately separate from official XML/AS4 contracts. Official wire-contract emulation and business pending/rejected tax outcomes remain a further adapter task after message/action mapping and required access/qualification are verified. No BAS lodgement or certification is claimed.
