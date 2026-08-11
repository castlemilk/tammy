# Tammy Mac App Store metadata

This worksheet is linked to the draft App Store Connect record created on 11 August 2026. Replace every remaining `OPERATOR_REQUIRED` value before upload and keep the copy aligned with the build under review.

## Product page

- **App Store Connect ID:** `6800226692`
- **Apple Developer identifier ID:** `DXP9QHD7JH`
- **Bundle identifier:** `com.tammy.desktop`
- **Name:** Tammy Accounting (the installed app name remains Tammy)
- **Subtitle:** Local accounting for Australia
- **Primary category:** Finance
- **Secondary category:** Business
- **Default language:** English (Australia)
- **SKU:** `tammy-macos-001`
- **Release method:** Manual
- **Privacy policy URL:** `OPERATOR_REQUIRED`
- **Support URL:** `OPERATOR_REQUIRED`
- **Marketing URL:** optional
- **Copyright:** `OPERATOR_REQUIRED`
- **Price and availability:** `OPERATOR_REQUIRED`
- **Age rating:** complete the App Store Connect questionnaire for the submitted build; do not select the Kids Category.

## Description

Tammy is local-first accounting software for Australian small businesses. Create an encrypted workspace, set up an organisation and chart of accounts, post balanced journals, review source documents, reconcile bank statement lines, prepare GST and BAS drafts, and inspect retained local activity.

Business data stays in the encrypted workspace on this Mac. Tammy does not require a cloud account and the current release does not include advertising, analytics, tracking, in-app purchases, or ATO lodgement.

## Keywords

`accounting,bookkeeping,BAS,GST,ledger,reconciliation,expenses,Australia`

## Review notes

Tammy is an offline desktop application. It has no remote demo account. On first launch, choose **Create encrypted workspace**, record the one-time recovery code shown by the app, confirm that it has been saved, and sign in with the locally created owner credentials. The reviewer can then create a fictional organisation and use the included Australian chart template.

The bundled `tammy-core` executable is supervised by Electron and serves only the authenticated loopback transport. The app does not download executable code or leave a background process running after quit. BAS screens create local drafts only; the build does not lodge with the ATO.

For review support contact: `OPERATOR_REQUIRED`.

## Screenshots

Provide one to ten screenshots showing the app in use, all at one accepted 16:10 Mac size: 1280×800, 1440×900, 2560×1600, or 2880×1800. Use only fictional Australian business data and do not use setup/sign-in as the sole screenshot.

Recommended sequence:

1. Overview with documents, banking, and BAS attention cards.
2. Reviewed source document with extracted accounting details.
3. Balanced journal and linked trial balance.
4. Banking reconciliation.
5. GST & BAS draft with clear “not lodged” status.

## App privacy and export compliance

- **Data collection:** None for the current offline build; verify against the signed-build Xcode privacy report.
- **Tracking:** None.
- **Third-party advertising/analytics:** None.
- **Encryption/export compliance:** `OPERATOR_REQUIRED` — obtain a legal/export-compliance determination for SQLCipher and TLS, then keep `ITSAppUsesNonExemptEncryption` and App Store Connect answers consistent.
- **Financial-services developer entity:** `OPERATOR_REQUIRED` — confirm submission by the legal entity responsible for Tammy.
