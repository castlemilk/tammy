export type PolicyInline =
  | { readonly type: "text" | "emphasis" | "code"; readonly value: string }
  | { readonly type: "link"; readonly text: string; readonly href: string };

export interface PolicySection {
  readonly heading: string;
  readonly blocks: readonly (
    | { readonly kind: "paragraph"; readonly inlines: readonly PolicyInline[] }
    | { readonly kind: "list"; readonly items: readonly (readonly PolicyInline[])[] }
  )[];
}

export const publicContent = {
  "identity": {
    "schemaVersion": 1,
    "appStoreName": "Tammy Accounting",
    "installedName": "Tammy",
    "bundleIdentifier": "com.tammy.desktop",
    "publisher": "Gamma Systems Pty Ltd",
    "supportEmail": "ben.ebsworth@gmail.com",
    "locale": "en-AU",
    "primaryCategory": "Finance",
    "secondaryCategory": "Business",
    "minimumMacOSVersion": "14.0",
    "architectures": [
      "arm64"
    ],
    "copyright": "© 2026 Gamma Systems Pty Ltd",
    "capabilityBoundary": {
      "reporting": "preparation-only",
      "atoLodgement": "not-lodged"
    }
  },
  "marketingVersion": "0.1.0",
  "policy": {
    "effectiveDate": "23 October 2026",
    "sections": [
      {
        "heading": "Publisher and scope",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Tammy is published by Gamma Systems Pty Ltd. This policy describes the current Tammy macOS app and public website. It is a plain-language description of current handling; it does not claim that Gamma Systems is an ATO-registered Digital Service Provider, that Tammy has completed the ATO security framework, or that the ATO endorses Tammy."
              }
            ]
          }
        ]
      },
      {
        "heading": "Information in your workspace",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "When you use Tammy, the encrypted workspace can contain business and personal information that you enter or choose to import. This can include organisation names and ABNs, contacts, customer and supplier details, invoices, receipts, source documents, bank transactions, accounting entries, tax profiles, BAS calculations and evidence, review decisions, user names and roles, and security or audit history. Business records may contain personal information about you, staff, customers, suppliers, or other people."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Tammy uses this information to run the local accounting and reporting features you choose. The app does not automatically send your workspace records to Gamma Systems or a Tammy cloud service. Your operating system, other software on your Mac, and people who can access your Mac or workspace may also be able to access local files or records, subject to the security controls you use."
              }
            ]
          }
        ]
      },
      {
        "heading": "Local storage and security",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Accounting records and imported evidence are stored in the encrypted workspace on your Mac. Workspace secrets and supported credentials are kept locally; some are stored in the macOS Keychain. The desktop app and its bundled core communicate over an authenticated local connection. Tammy does not provide an online account, cloud workspace, or cross-device sync in this release."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "These controls reduce exposure but cannot protect data from someone who can use your unlocked Mac, access your workspace credentials, or control other software on your device. Keep macOS and Tammy updated, lock your Mac when unattended, and do not share passwords, recovery codes, TOTP codes, machine credentials, or connection files."
              }
            ]
          }
        ]
      },
      {
        "heading": "Files, reports, and local integrations",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Tammy reads a file only after you choose it for import. A file you import can be retained as source evidence in the encrypted workspace. When you export a report, Tammy writes the file to the location you choose. The exported copy is outside Tammy's encrypted workspace and is your responsibility to protect and remove."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Local API and MCP access is off unless you explicitly enable it from Tammy's native menu. If enabled, Tammy creates a private connection file and a local broker. A client on the same Mac that can read that file can access the connected organisation's permitted data and stage an editable invoice proposal for you to review in Tammy. The client cannot save, issue, post, or send an invoice through this connection. Do not share the connection file. Access ends when you revoke it, sign in again, or close Tammy."
              }
            ]
          }
        ]
      },
      {
        "heading": "ABN and entity checks",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "You enter your organisation's ABN in Tammy. Tammy checks its format and checksum locally; it does not query the Australian Business Register in the background, use "
              },
              {
                "type": "link",
                "text": "ABN Lookup web services",
                "href": "https://abr.business.gov.au/Tools/WebServices"
              },
              {
                "type": "text",
                "value": ", verify registration status online, or fill organisation details from ABR data. When the checksum passes, setup offers an optional \"Look up this ABN on the Australian Business Register\" link. If you select it, your browser opens the official "
              },
              {
                "type": "link",
                "text": "ABN Lookup page",
                "href": "https://abr.business.gov.au/"
              },
              {
                "type": "text",
                "value": " with that ABN in the URL, and ABR receives it to perform the lookup. Tammy does not import the result; you must review the details on ABR's page and enter or record information yourself. Entity verification in Tammy records information and an extract that a workspace administrator has independently reviewed. It is a user-recorded result, not an online ABR verification by Tammy."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "ABN Lookup publishes some business information, which may not be complete or current; it does not publish detailed contact information. Check important details with the business or another source."
              }
            ]
          }
        ]
      },
      {
        "heading": "ATO, RAM, and SBR",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "myID is a Digital ID used by a person to prove their identity. Relationship Authorisation Manager, or RAM, lets a principal authority or authorisation administrator grant that person access to business services. An SBR machine credential is a separate credential used by supported software to interact with government services."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "ATO access for software developers is a separate process. It uses Online services for Digital Service Providers and a RAM authorisation for that service. The ATO's Digital Service Provider Operational Security Framework has its own risk-scaled security requirements. A customer RAM authorisation or a locally imported machine credential does not mean that Gamma Systems or Tammy is registered, approved, or compliant as a Digital Service Provider."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "This release's SBR readiness checks use a synthetic, network-disabled simulator. Tammy does not send BAS or other tax submissions to the ATO or SBR in this release. Preparing or freezing a BAS report does not lodge it, and does not establish ATO approval or acceptance. See the ATO information about "
              },
              {
                "type": "link",
                "text": "myID, RAM, and machine credentials",
                "href": "https://softwaredevelopers.ato.gov.au/usingmygovidramandmachinecredentials"
              },
              {
                "type": "text",
                "value": ", "
              },
              {
                "type": "link",
                "text": "Online services for Digital Service Providers",
                "href": "https://softwaredevelopers.ato.gov.au/OnlineservicesforDSPs"
              },
              {
                "type": "text",
                "value": ", and the "
              },
              {
                "type": "link",
                "text": "Digital Service Provider security framework",
                "href": "https://softwaredevelopers.ato.gov.au/operational_framework/meeting-requirements"
              },
              {
                "type": "text",
                "value": "."
              }
            ]
          }
        ]
      },
      {
        "heading": "Website, email, and service providers",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "The public website has no Tammy account or form and does not include advertising or analytics. The public website's hosting provider may process technical request information such as an IP address, browser details, requested pages, and security events to serve and protect the site."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Support email is received at Gmail. If you email us, Gamma Systems receives the message and Google processes it as the email service provider. Your own email provider may also process the message. Providers may process information in more than one country; we have not verified a complete list of processing or storage countries for these services. Consult the providers' current privacy information for their handling and locations."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "The app does not include Gamma-owned analytics, advertising, or tracking. If you choose to open an official website, send a support email, export a report, or enable a local integration, information is shared only as described for that action above."
              }
            ]
          }
        ]
      },
      {
        "heading": "Retention, backups, and deletion",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Workspace records remain on your Mac until you remove them. Tammy does not provide an online backup service. If you make copies using macOS or other software, those copies are outside Tammy's control and may remain after you remove a local workspace."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Important accounting, approval, and audit records are retained as linked history. Some records cannot be edited or deleted in place; a correction may be recorded as a new linked entry so the earlier decision remains visible. Removing the Tammy app alone does not remove its encrypted workspace or Keychain items. The "
              },
              {
                "type": "link",
                "text": "support page",
                "href": "https://tammy-accounting.castlemilk.chatgpt.site/support"
              },
              {
                "type": "text",
                "value": " describes tested local cleanup steps."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "During workspace setup Tammy may display a one-time recovery code. Store it somewhere secure and separate from your Mac. Support cannot use it to access your workspace, and Gamma Systems cannot recover local records remotely if your access information or workspace is lost."
              }
            ]
          }
        ]
      },
      {
        "heading": "Access, correction, and privacy complaints",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Your workspace is local to your Mac, and Gamma Systems cannot open or change it remotely. You can review and correct current records using Tammy's available controls. Linked BAS approval and audit history is preserved; corrections to those records are recorded as new entries rather than rewriting the earlier history."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "To ask about personal information in a support email, request access or correction, or raise a privacy concern, email "
              },
              {
                "type": "link",
                "text": "ben.ebsworth@gmail.com",
                "href": "mailto:ben.ebsworth@gmail.com"
              },
              {
                "type": "text",
                "value": " with the subject line Privacy. Describe the request without attaching accounting records or credentials. We will review it and respond; we do not promise a particular response time in this policy. If a concern is covered by the Privacy Act and remains unresolved, you may contact the "
              },
              {
                "type": "link",
                "text": "Office of the Australian Information Commissioner",
                "href": "https://www.oaic.gov.au/privacy/privacy-complaints"
              },
              {
                "type": "text",
                "value": "."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Do not email accounting data, source documents, passwords, recovery codes, TOTP codes, machine credentials, or cryptographic keys."
              }
            ]
          }
        ]
      },
      {
        "heading": "Contact and changes",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Gamma Systems Pty Ltd is the publisher. The current published privacy and support contact is "
              },
              {
                "type": "link",
                "text": "ben.ebsworth@gmail.com",
                "href": "mailto:ben.ebsworth@gmail.com"
              },
              {
                "type": "text",
                "value": ". This policy will be updated when Tammy's data handling changes materially. The effective date at the top shows when this version took effect."
              }
            ]
          }
        ]
      }
    ]
  },
  "deletionGuidance": {
    "containerDisplayPath": "~/Library/Containers/com.tammy.desktop",
    "groupContainerSuffix": "com.tammy.desktop",
    "keychainServices": [
      "com.tammy.workspace",
      "com.tammy.attempt-journal-anchor.v1",
      "com.tammy.audit-mirror",
      "com.tammy.sbr.production"
    ]
  }
} as const;
