// Package docscheck holds the tests that keep docs/evals/sponsors.json, docs/evals/lab-log.json and the logos in
// docs/evals/sponsors/ within their published rules. It has no code of its own: a pull request that adds a sponsor,
// ends a credit or publishes a month passes through these tests in CI before the site reads the files.
package docscheck
