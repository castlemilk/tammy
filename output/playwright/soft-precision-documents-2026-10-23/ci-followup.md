# Documents keyboard-scroll follow-up

Original head: `d1174f6e01b3364f06f223455d9ac919ba62955c`.

The original [PR macOS job 120502960080](https://github.com/castlemilk/tammy/actions/runs/39979562973/job/120502960080)
failed in the new Documents journey at line 173: after ArrowRight, `scrollLeft`
remained zero. Six packaged cases passed, this seventh case failed and 13 did
not run. Both Ubuntu and both Windows Server checks passed. The original push
macOS job remains active as this follow-up is prepared; no retry was requested.

The retained [artifact 12512389772](https://github.com/castlemilk/tammy/actions/runs/39979562973/artifacts/12512389772)
has SHA256 `33dad84e919586ab6085c8994e0d265804952a190fb5b6a0b4892996664c8914`.
Its context marks the labelled Documents table region active, and the inspected
320px screenshot shows visible focus and an available horizontal scrollbar at
the left edge. The unchanged synthetic screenshot is retained in
`hosted-first-failure/320px-focused-region.png`; raw logs/traces remain outside
the repository. No TOTP or other authentication material is included.

Local macOS 26.5 captures and the connected native journey had passed the same
check. The evidence establishes an inconsistent native keyboard-scroll path
after resize on the hosted macOS 14 run; it does not establish Chromium's
underlying internal cause. The existing shared overflow component relied
entirely on browser default key routing.

The correction gives an overflowing region explicit, immediate 80px horizontal
steps for unmodified ArrowLeft/ArrowRight only when the region itself is the
event target. It preserves child controls, modified keys, vertical navigation
and regions without horizontal overflow. It adds no global listener or financial
handler. The native journey retains its movement assertion and additionally
asserts focus and actual overflow before sending the key. No delay, timeout,
retry, test omission or release gate is added.

The focused regression fails on the old component (one failed, two passed), then
passes with the correction. Documents/purchase/scroller checks pass 55 tests in
six files in 11.13s. The complete desktop suite passes **1,308 tests in 111
files**, zero skips, in **132.11s**. Typecheck and lint pass with the same 135
warnings and 12 infos. The strict audit retains the same 19 findings.

The first 12 after frames, 12 before frames, three baseline-attempt frames and
unchanged concept are preserved. Corrected runtime captures use the separate
`after-keyboard-fix` directory; the original images are not overwritten or
relabeled. Fresh native/package and exact-head CI outcomes are reported in the
subsequent evidence and draft PR after validation.
