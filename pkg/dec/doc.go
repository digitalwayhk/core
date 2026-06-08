// Deprecated: Package dec is a legacy in-process event bus (publish/subscribe)
// that predates the framework's cluster-aware event subsystem.
//
// New code should use pkg/server/event instead:
//
//	sc.EventStream.Subscribe(eventType, handler)   // in-process
//	sc.NewEventPublisher(subject).Publish(ctx, env) // cross-node via MQ
//
// This package will be removed in a future release. No new features will be added.
package dec
