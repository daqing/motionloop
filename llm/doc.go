// Package llm defines motionloop's unified LLM vocabulary: the message model
// shared by providers, the agent runtime, and session persistence, plus
// provider-neutral stream events and model metadata.
//
// It plays the role pi-ai plays in pi. Providers translate between these
// types and their wire protocols; sessions persist them verbatim using the
// pi-compatible entry schema.
package llm
