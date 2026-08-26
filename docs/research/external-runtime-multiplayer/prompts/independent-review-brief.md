# Independent review prompt

Review the proposed or implemented Bindery external-runtime multiplayer slice
against the research pack. Do not assess whether it resembles a commercially
complete multiplayer service; assess whether it answers the stated research
question with the least machinery that produces credible evidence.

Check, in this order:

1. **Boundary integrity**
   - Does Bindery remain control/transport/observation rather than simulation?
   - Did RA2-specific behavior leak into core contracts?
   - Did lobby, account recovery, ranking, anti-cheat or AI work enter scope?

2. **Public-data integrity**
   - Are all application records public as promised?
   - Are tokens, verifiers, raw endpoints and authorization metadata excluded?
   - Can public DTOs accidentally serialize internal database/Kubernetes fields?

3. **External-client semantics**
   - Are client class and client instance separate concepts?
   - Can two players enroll without an observer?
   - Can an observer be added without changing the player contract?
   - Does lost client state converge rather than require manual repair?

4. **Telemetry epistemology**
   - Are observations attributed and immutable?
   - Are contradictions preserved?
   - Are derivations replayable from versioned raw inputs?
   - Can ingest or normalization fail without affecting game traffic?

5. **Relay lifecycle**
   - Is allocation pinned to one addressable relay?
   - Does scale-in drain instead of migrate?
   - Are allocation and scaling based on session/packet/egress signals rather than CPU alone?
   - Are rate, packet-size and lease limits present without being mislabeled anti-cheat?

6. **Bindery fit**
   - Is `WorldInstance` use supported by actual lifecycle evidence?
   - Are relay replicas kept distinct from `WorldShard`?
   - Does any proposed CRD represent durable reconciled intent, or merely mirror a connection table?

Return:

- verdict: proceed, revise, or stop;
- findings ordered by severity with requirement/ADR references;
- exact evidence for each finding;
- smallest corrective change;
- tests needed to close uncertainty;
- any part that should move to an adapter/sibling repository.

