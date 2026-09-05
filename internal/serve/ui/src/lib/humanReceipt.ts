/** The small identifier-only request accepted by the native Tusker shell. */
export interface HumanReceiptRequest {
  projectId: string;
  gateId: string;
  action: "satisfy";
}

/** Native owns the challenge, signature, and submit; JS only sees the outcome. */
export interface HumanReceiptBridgeResult {
  status: "accepted" | "cancelled" | "error";
  message?: string;
}
