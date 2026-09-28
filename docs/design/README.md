# Design records

Point-in-time design documents and implementation plans, kept for the reasoning behind the
Cookie Consent publication workflow. They describe the provider as it was designed on the date in
their file name, not as it is today; where they disagree with the guides in `docs/`, the
[README](../../README.md), or the [CHANGELOG](../../CHANGELOG.md), the current documentation is
authoritative.

Known differences since these were written:

- Stack configuration now takes precedence over the `OSANO_*` environment variables
  ([installation and configuration](../installation-configuration.md#configuration-reference)).
- Config and rule creates are retried only after `429`, never after a `5xx`, and a lost config
  create is adopted when unambiguous ([state management](../state-management.md#cookie-consent-lifecycle)).
- Rule IDs are always `<configId>/<ruleId>`; the numeric-only form of pre-release builds is gone.
- The provider builds with Go 1.27 and pulumi-go-provider v1.6.

| Date | Document |
| --- | --- |
| 2026-08-13 | [Cookie Consent publication and script delivery design](specs/2026-08-13-cookie-consent-publication-design.md) |
| 2026-08-13 | [Cookie Consent publication implementation plan](plans/2026-08-13-cookie-consent-publication.md) |
