// Package acceptance holds the tofu-driven tests of the RcodeZero provider. They
// run real tofu commands against the in-process mock and only run with
// TF_ACC=1 (see `make testacc`); the unit tests live next to the code in
// internal/provider.
package acceptance
