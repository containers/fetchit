# Data Model: Podman v5 Migration

**Feature**: Podman v5 Dependency Upgrade
**Date**: 2025-12-30

## Overview

This is a **dependency upgrade**, not a feature adding new data models. Fetchit uses Podman's existing data structures and does not define custom persistent data models. This document tracks changes to Podman types used by fetchit.

## Podman Type Usage in Fetchit

### Container Specification Types

**Source Package**: `github.com/containers/podman/v5/pkg/specgen`

**Current v4 Usage**:
```go
type SpecGenerator struct {
    Name       string
    Privileged bool
    PidNS      Namespace
    Command    []string
    Mounts     []specs.Mount
    Volumes    []*NamedVolume
    Devices    []specs.LinuxDevice
    // ... other fields
}

type Namespace struct {
    NSMode string
    Value  string
}

type NamedVolume struct {
    Name    string
    Dest    string
    Options []string
}
```

**Expected v5 Changes**: TBD based on research findings
- Field renames or type changes
- New required fields
- Deprecated fields to remove

### Container Status Types

**Source Package**: `github.com/containers/podman/v5/libpod/define` (or new location in v5)

**Current v4 Usage**:
```go
const stopped = define.ContainerStateStopped
```

**Expected v5 Changes**: TBD based on research findings
- Package relocation
- Enum value changes
- New status states

### Container Operation Types

**Source Package**: `github.com/containers/podman/v5/pkg/bindings/containers`

**Current v4 Usage**:
```go
entities.ContainerCreateResponse - Response from CreateWithSpec
containers.WaitOptions - Options for Wait operation
containers.RemoveOptions - Options for Remove operation
```

**Expected v5 Changes**: TBD based on research findings
- Response structure changes
- Option structure changes
- New required parameters

### Image Operation Types

**Source Package**: `github.com/containers/podman/v5/pkg/bindings/images`

**Current v4 Usage**:
- Image pull responses
- Image inspection structures
- Image load responses

**Expected v5 Changes**: TBD based on research findings

## Type Migration Mapping

This section will be filled after research phase completes:

| v4 Type | v5 Type | Changes | Impact on Fetchit |
|---------|---------|---------|-------------------|
| TBD | TBD | TBD | TBD |

## Validation Rules

**No custom validation needed** - Fetchit relies on Podman's type validation. Changes:
- Update to v5 validation logic
- Handle new error types from v5 APIs
- Test with v5 type constraints

## State Transitions

**No custom state management** - Fetchit uses Podman container states:
- Created → Started → Running → Stopped → Removed
- No changes expected to state transition model
- Verify v5 state machine matches v4

## Impact Analysis

### High Impact Areas
1. **pkg/engine/container.go**: Uses SpecGenerator extensively
2. **pkg/engine/raw.go**: Creates container specs
3. **pkg/engine/image.go**: Uses image operation types

### Medium Impact Areas
1. **pkg/engine/kube.go**: May use pod-related types
2. **pkg/engine/apply.go**: Uses container operation responses

### Low Impact Areas
1. **pkg/engine/config.go**: Configuration types (likely unchanged)
2. **pkg/engine/gitauth.go**: Git operations (no Podman types)

## Next Steps

1. Complete research phase to identify actual v5 type changes
2. Update this document with concrete type mappings
3. Create migration checklist for each affected file
4. Generate unit tests to verify type compatibility
