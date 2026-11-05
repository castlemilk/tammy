# Gamma components vendored into Tammy

The `SegmentedControl` source is copied from the user's maintained Gamma catalogue as vendored in [Shorted](https://github.com/castlemilk/shorted.com.au/tree/8afef93e72e94c54a4841b6d7254cefe9dc05515/web/src/%40/components/gamma), based on `~/projects/gamma/src/components/SegmentedControl.tsx`. It remains owner-controlled source, not an unrelated framework or invented look-alike.

- `segmented-control.tsx`: exact Gamma native-radio-group React component, with controlled and uncontrolled selection.
- `segmented-control.css`: the exact Gamma `/* Segmented control */` CSS section from the source `gamma.css`, followed by Tammy-only 44px target and narrow-screen layout adaptations.
- `app/design-system.css`: Gamma token bridge to Tammy's Soft Precision palette; no independent Gamma colour theme.
- No new runtime UI dependency. The original Gamma selection semantics use native labelled radio inputs, with keyboard handling and reduced-motion styles.

When updating, copy the maintained Gamma component and corresponding CSS section again; keep Tammy-specific adjustments in the marked block. Avoid importing the entire Gamma CSS catalogue for one control. All controls must have useful labels and be tested in the target workflow.
