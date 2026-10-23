# Tammy privacy policy

Effective 23 October 2026.

## Publisher and scope

Tammy is published by Gamma Systems Pty Ltd. This policy describes the current Tammy macOS app and public website. It is a plain-language description of current handling; it does not claim that Gamma Systems is an ATO-registered Digital Service Provider, that Tammy has completed the ATO security framework, or that the ATO endorses Tammy.

## Information in your workspace

When you use Tammy, the encrypted workspace can contain business and personal information that you enter or choose to import. This can include organisation names and ABNs, contacts, customer and supplier details, invoices, receipts, source documents, bank transactions, accounting entries, tax profiles, BAS calculations and evidence, review decisions, user names and roles, and security or audit history. Business records may contain personal information about you, staff, customers, suppliers, or other people.

Tammy uses this information to run the local accounting and reporting features you choose. The app does not automatically send your workspace records to Gamma Systems or a Tammy cloud service. Your operating system, other software on your Mac, and people who can access your Mac or workspace may also be able to access local files or records, subject to the security controls you use.

## Local storage and security

Accounting records and imported evidence are stored in the encrypted workspace on your Mac. Workspace secrets and supported credentials are kept locally; some are stored in the macOS Keychain. The desktop app and its bundled core communicate over an authenticated local connection. Tammy does not provide an online account, cloud workspace, or cross-device sync in this release.

These controls reduce exposure but cannot protect data from someone who can use your unlocked Mac, access your workspace credentials, or control other software on your device. Keep macOS and Tammy updated, lock your Mac when unattended, and do not share passwords, recovery codes, TOTP codes, machine credentials, or connection files.

## Files, reports, and local integrations

Tammy reads a file only after you choose it for import. A file you import can be retained as source evidence in the encrypted workspace. When you export a report, Tammy writes the file to the location you choose. The exported copy is outside Tammy's encrypted workspace and is your responsibility to protect and remove.

Local API and MCP access is off unless you explicitly enable it from Tammy's native menu. If enabled, Tammy creates a private connection file and a local broker. A client on the same Mac that can read that file can access the connected organisation's permitted data and stage an editable invoice proposal for you to review in Tammy. The client cannot save, issue, post, or send an invoice through this connection. Do not share the connection file. Access ends when you revoke it, sign in again, or close Tammy.

## ABN and entity checks

You enter your organisation's ABN in Tammy. Tammy checks its format and checksum locally; it does not query the Australian Business Register in the background, use [ABN Lookup web services](https://abr.business.gov.au/Tools/WebServices), verify registration status online, or fill organisation details from ABR data. When the checksum passes, setup offers an optional "Look up this ABN on the Australian Business Register" link. If you select it, your browser opens the official [ABN Lookup page](https://abr.business.gov.au/) with that ABN in the URL, and ABR receives it to perform the lookup. Tammy does not import the result; you must review the details on ABR's page and enter or record information yourself. Entity verification in Tammy records information and an extract that a workspace administrator has independently reviewed. It is a user-recorded result, not an online ABR verification by Tammy.

ABN Lookup publishes some business information, which may not be complete or current; it does not publish detailed contact information. Check important details with the business or another source.

## ATO, RAM, and SBR

myID is a Digital ID used by a person to prove their identity. Relationship Authorisation Manager, or RAM, lets a principal authority or authorisation administrator grant that person access to business services. An SBR machine credential is a separate credential used by supported software to interact with government services.

ATO access for software developers is a separate process. It uses Online services for Digital Service Providers and a RAM authorisation for that service. The ATO's Digital Service Provider Operational Security Framework has its own risk-scaled security requirements. A customer RAM authorisation or a locally imported machine credential does not mean that Gamma Systems or Tammy is registered, approved, or compliant as a Digital Service Provider.

This release's SBR readiness checks use a synthetic, network-disabled simulator. Tammy does not send BAS or other tax submissions to the ATO or SBR in this release. Preparing or freezing a BAS report does not lodge it, and does not establish ATO approval or acceptance. See the ATO information about [myID, RAM, and machine credentials](https://softwaredevelopers.ato.gov.au/usingmygovidramandmachinecredentials), [Online services for Digital Service Providers](https://softwaredevelopers.ato.gov.au/OnlineservicesforDSPs), and the [Digital Service Provider security framework](https://softwaredevelopers.ato.gov.au/operational_framework/meeting-requirements).

## Website, email, and service providers

The public website has no Tammy account or form and does not include advertising or analytics. The public website's hosting provider may process technical request information such as an IP address, browser details, requested pages, and security events to serve and protect the site.

Support email is received at Gmail. If you email us, Gamma Systems receives the message and Google processes it as the email service provider. Your own email provider may also process the message. Providers may process information in more than one country; we have not verified a complete list of processing or storage countries for these services. Consult the providers' current privacy information for their handling and locations.

The app does not include Gamma-owned analytics, advertising, or tracking. If you choose to open an official website, send a support email, export a report, or enable a local integration, information is shared only as described for that action above.

## Retention, backups, and deletion

Workspace records remain on your Mac until you remove them. Tammy does not provide an online backup service. If you make copies using macOS or other software, those copies are outside Tammy's control and may remain after you remove a local workspace.

Important accounting, approval, and audit records are retained as linked history. Some records cannot be edited or deleted in place; a correction may be recorded as a new linked entry so the earlier decision remains visible. Removing the Tammy app alone does not remove its encrypted workspace or Keychain items. The [support page](https://tammy-accounting.castlemilk.chatgpt.site/support) describes tested local cleanup steps.

During workspace setup Tammy may display a one-time recovery code. Store it somewhere secure and separate from your Mac. Support cannot use it to access your workspace, and Gamma Systems cannot recover local records remotely if your access information or workspace is lost.

## Access, correction, and privacy complaints

Your workspace is local to your Mac, and Gamma Systems cannot open or change it remotely. You can review and correct current records using Tammy's available controls. Linked BAS approval and audit history is preserved; corrections to those records are recorded as new entries rather than rewriting the earlier history.

To ask about personal information in a support email, request access or correction, or raise a privacy concern, email [ben.ebsworth@gmail.com](mailto:ben.ebsworth@gmail.com) with the subject line Privacy. Describe the request without attaching accounting records or credentials. We will review it and respond; we do not promise a particular response time in this policy. If a concern is covered by the Privacy Act and remains unresolved, you may contact the [Office of the Australian Information Commissioner](https://www.oaic.gov.au/privacy/privacy-complaints).

Do not email accounting data, source documents, passwords, recovery codes, TOTP codes, machine credentials, or cryptographic keys.

## Contact and changes

Gamma Systems Pty Ltd is the publisher. The current published privacy and support contact is [ben.ebsworth@gmail.com](mailto:ben.ebsworth@gmail.com). This policy will be updated when Tammy's data handling changes materially. The effective date at the top shows when this version took effect.
