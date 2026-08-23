# Bindery hybrid-worker packet rules

- Every packet uses `agentops-task/v2`, one attempt, no network, and an exact
  starting commit.
- A live Sprintctl/Vuoro reservation is required before dispatch.
- The worker identity must be separately provisioned and must not be able to
  write the coordinator checkout.
- Worker receipts remain `qualification_eligible: false`; the local inference
  exception is project-scoped and does not qualify or promote the model.
- Rejection returns to the coordinator. Retry requires a materially revised
  packet and a new starting commit/reservation.

