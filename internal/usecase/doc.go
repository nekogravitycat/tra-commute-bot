// Package usecase orchestrates one run of the commute brief.
//
// It owns the sequence — guard, fetch, plan, render, notify, record — and it
// declares the interfaces it needs to do so. It does not implement any of them:
// the concrete TDX client, Telegram sender, state file and clock all live in
// internal/adapter and internal/platform and are injected by cmd/tracommute.
// Dependencies therefore point inwards only, and this package can be tested
// against fakes without a network.
package usecase
