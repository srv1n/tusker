package main

import "testing"

func TestRunnerRecoveryConformance(t *testing.T) {
	t.Run("unstarted claim is requeued", TestDirectedClaimRecoveryRequeuesUnstartedIntent)
	t.Run("spawned claim is fenced", TestDirectedClaimRecoveryDoesNotRequeueSpawnedIntent)
	t.Run("spawn registration race is atomic", TestDirectedClaimSpawnRegistrationRacesRecovery)
	t.Run("early exit is classified", TestEarlyExitClassification)
}
