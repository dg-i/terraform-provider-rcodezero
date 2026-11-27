<!--
Thanks for contributing! Please complete the sections below.
Sections marked "delete if n/a" can be removed when they don't apply.
-->

## Summary

<!-- One or two sentences explaining what this PR changes and why. -->

## Related issue

<!-- e.g. Closes #123, Refs #456. Delete if n/a. -->

## Type of change

- [ ] Bug fix
- [ ] New feature (new resource, data source, or attribute)
- [ ] Schema change (breaking or backwards-compatible)
- [ ] Refactor / internal change (no user-visible behavior change)
- [ ] Documentation
- [ ] CI / tooling

## Test plan

<!--
Bulleted checklist of what you verified. Be specific — "ran make test" is
fine, but "added unit test covering the X edge case in foo_test.go" is
better.
-->

- [ ] `make test` passes
- [ ] `make testacc` passes (tofu against the in-process mock, no token needed)
- [ ] `make lint` passes
- [ ] `make generate` re-run (if schema, examples or `api/` changed) — generated code and `docs/` are up to date
- [ ] Manual examples in `README.md` still valid (if schema changed)

## CHANGELOG

<!--
If this PR is user-visible, add an entry under `## Unreleased` in
CHANGELOG.md (FEATURES / ENHANCEMENTS / BUG FIXES / INTERNAL). Delete this
section if the change is purely internal/CI/docs.
-->

- [ ] CHANGELOG.md updated

## Notes for reviewers

<!--
Anything reviewers should pay extra attention to: tricky areas, deferred
follow-ups, decisions you'd like a second opinion on. Delete if n/a.
-->
