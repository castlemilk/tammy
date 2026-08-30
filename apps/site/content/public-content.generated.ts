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
    "effectiveDate": "30 August 2026",
    "sections": [
      {
        "heading": "Publisher and scope",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Tammy Accounting is published by Gamma Systems Pty Ltd. This policy applies to the current macOS release of Tammy and the Tammy public website."
              }
            ]
          }
        ]
      },
      {
        "heading": "Data handled by Tammy",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "For this release, Tammy does not transmit your accounting records, credentials, analytics, advertising identifiers, or tracking data to Gamma Systems Pty Ltd or third parties."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Accounting records are stored locally in Tammy's encrypted workspace on your Mac. Workspace secrets are stored in the macOS Keychain where applicable. Files you choose to import are read only after you select them and their accounting content remains in the local encrypted workspace."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Tammy's bundled core communicates with the desktop application over an authenticated local connection. It is not a cloud service. The app opens this privacy policy and the support page in your browser only when you choose those links."
              }
            ]
          }
        ]
      },
      {
        "heading": "Website and hosting",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "The Tammy public website has no Gamma-owned analytics, cookies, accounts, or forms. Hosting infrastructure may process request and security logs to operate and protect the site."
              }
            ]
          }
        ]
      },
      {
        "heading": "Support email",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Support is available at "
              },
              {
                "type": "link",
                "text": "ben.ebsworth@gmail.com",
                "href": "mailto:ben.ebsworth@gmail.com"
              },
              {
                "type": "text",
                "value": ". Sending an email is user initiated and messages are processed by the relevant email providers, not by the Tammy app."
              }
            ]
          },
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Do not email accounting data, recovery codes, passwords, machine credentials, or cryptographic keys."
              }
            ]
          }
        ]
      },
      {
        "heading": "Retention and deletion",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Local records remain on your Mac until you remove them. Removing the Tammy app alone does not promise deletion of its encrypted workspace or macOS Keychain items. For tested macOS cleanup steps, see "
              },
              {
                "type": "link",
                "text": "/support",
                "href": "https://tammy-accounting.castlemilk.chatgpt.site/support"
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
        "heading": "Reporting boundary",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Tammy currently prepares reporting information only. It does not lodge ATO or SBR submissions in this release."
              }
            ]
          }
        ]
      },
      {
        "heading": "Changes to this policy",
        "blocks": [
          {
            "kind": "paragraph",
            "inlines": [
              {
                "type": "text",
                "value": "Material changes to Tammy's data handling will be reflected in the app, its privacy manifest, its App Store privacy disclosures, and this policy before release."
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
