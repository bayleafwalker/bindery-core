# Bindery external-runtime coordinator boundaries

The coordinator owns public/authenticated DTO semantics, credential storage and
rotation, state-machine transitions, redaction and serialization oracles,
datastore interfaces, relay authentication, placement policy, failure
semantics, CRD alignment, security policy, release metadata, and rollout.

Workers may implement beneath frozen interfaces and coordinator-authored tests:
mechanical DTO conversion, repository methods after interfaces are frozen,
fixture builders, codec mechanics, metrics plumbing, and schema-example
synchronization. Workers may not modify tests, schemas, migrations,
credentials, deployment manifests, release files, or security policy.

