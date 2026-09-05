import CryptoKit
import XCTest
@testable import TuskerBar

final class HumanDecisionReceiptTests: XCTestCase {
    private func request() throws -> HumanReceiptRequest {
        try HumanReceiptRequest(projectID: "project-1", gateID: "FLW-G-0010", action: "satisfy")
    }

    private func challenge(expiresAt: String = "2030-01-02T03:04:05.123456789Z") -> HumanReceiptChallenge {
        HumanReceiptChallenge(
            id: "challenge-1", projectID: "project-1", gateID: "FLW-G-0010", actor: "human:operator",
            materialRevision: "sha256:material", actionDigest: "sha256:action", nonce: "nonce-1", expiresAt: expiresAt,
            gateTitle: "Approve deploy", actionText: "Deploy revision abc", verificationText: "Production account owner approval"
        )
    }

    func testCanonicalPayloadAndDERP256Signature() throws {
        let challenge = challenge()
        _ = try challenge.validate(for: request(), now: Date(timeIntervalSince1970: 0))
        let receipt = try HumanDecisionReceipt(challenge: challenge, issuedAt: "2026-09-05T00:00:00.000Z", keyID: "sha256:key")
        XCTAssertEqual(String(decoding: try receipt.canonicalData(), as: UTF8.self), """
        tusker.human-receipt/v1
        challenge-1
        project-1
        FLW-G-0010
        human:operator
        sha256:material
        sha256:action
        accept
        nonce-1
        2026-09-05T00:00:00.000Z
        2030-01-02T03:04:05.123456789Z
        """)
        let key = P256.Signing.PrivateKey()
        let signature = try key.signature(for: receipt.canonicalData())
        let parsed = try P256.Signing.ECDSASignature(derRepresentation: signature.derRepresentation)
        XCTAssertTrue(key.publicKey.isValidSignature(parsed, for: try receipt.canonicalData()))
        XCTAssertFalse(signature.derRepresentation.base64EncodedString().trimmingCharacters(in: CharacterSet(charactersIn: "=")).contains("="))
    }

    func testStaleAndCrossGateChallengesFailClosed() throws {
        XCTAssertThrowsError(try challenge(expiresAt: "2020-01-01T00:00:00Z").validate(for: request(), now: Date(timeIntervalSince1970: 1_700_000_000))) { error in
            XCTAssertEqual(error as? HumanReceiptError, .expired)
        }
        XCTAssertThrowsError(try HumanReceiptChallenge(
            id: "challenge-1", projectID: "project-1", gateID: "OTHER-G-0010", actor: "human:operator",
            materialRevision: "sha256:material", actionDigest: "sha256:action", nonce: "nonce-1", expiresAt: "2030-01-02T03:04:05Z",
            gateTitle: "Approve deploy", actionText: "Deploy", verificationText: "Owner approval"
        ).validate(for: request(), now: Date(timeIntervalSince1970: 0)))
    }

    func testMalformedCanonicalFieldAndCancellationDoNotAuthorize() throws {
        let malformed = HumanReceiptChallenge(
            id: "challenge-1", projectID: "project-1", gateID: "FLW-G-0010", actor: "human:operator\nagent:forged",
            materialRevision: "sha256:material", actionDigest: "sha256:action", nonce: "nonce-1", expiresAt: "2030-01-02T03:04:05Z",
            gateTitle: "Approve deploy", actionText: "Deploy", verificationText: "Owner approval"
        )
        XCTAssertThrowsError(try malformed.validate(for: request(), now: Date(timeIntervalSince1970: 0)))
        XCTAssertFalse(HumanReceiptApproval.accepts(.cancel))
        XCTAssertTrue(HumanReceiptApproval.accepts(.alertFirstButtonReturn))
    }

    func testNativeOperatorUsesValidOverrideOrMacOSAccount() {
        XCTAssertEqual(HumanReceiptNativeOperator.resolve(environment: ["TUSKER_SERVE_OPERATOR": " HUMAN: configured "], username: "local-user"), "human:configured")
        XCTAssertEqual(HumanReceiptNativeOperator.resolve(environment: ["TUSKER_SERVE_OPERATOR": "agent:forged", "TUSKER_ACTOR": "human:secondary"], username: "local-user"), "human:secondary")
        XCTAssertEqual(HumanReceiptNativeOperator.resolve(environment: [:], username: "local-user"), "human:local-user")
        XCTAssertNil(HumanReceiptNativeOperator.resolve(environment: [:], username: "   "))
    }

    @MainActor
    func testCandidateServeProtocolUsesCachedCapabilityAndDirectChallenge() async throws {
        HumanReceiptURLProtocol.requests = []
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [HumanReceiptURLProtocol.self]
        let transport = HumanReceiptTransport(baseURL: URL(string: "http://127.0.0.1:7420")!, session: URLSession(configuration: configuration))
        let receiptRequest = try request()
        let receivedChallenge = try await transport.fetchChallenge(receiptRequest)
        let receipt = try HumanDecisionReceipt(challenge: receivedChallenge, issuedAt: "2026-09-05T00:00:00.000Z", keyID: "sha256:key")
        try await transport.submit(HumanReceiptSubmission(receipt: receipt, signature: "der-signature"))

        let requests = HumanReceiptURLProtocol.requests
        XCTAssertEqual(requests.filter { $0.path == "/api/capability" }.count, 1)
        let mutations = requests.filter { $0.method == "POST" }
        XCTAssertEqual(mutations.count, 2)
        XCTAssertTrue(mutations.allSatisfy { $0.capability == "candidate-capability" })
        let challengeData = try XCTUnwrap(mutations.first?.body)
        let challengeBody = try JSONSerialization.jsonObject(with: challengeData) as? [String: Any]
        XCTAssertEqual(challengeBody?["projectId"] as? String, "project-1")
        XCTAssertEqual(challengeBody?["gateId"] as? String, "FLW-G-0010")
        XCTAssertEqual(challengeBody?["action"] as? String, "satisfy")
        let submitData = try XCTUnwrap(mutations.last?.body)
        let submitBody = try JSONSerialization.jsonObject(with: submitData) as? [String: Any]
        XCTAssertNil(submitBody?["action"])
        XCTAssertEqual(submitBody?["projectId"] as? String, "project-1")
    }
}

private final class HumanReceiptURLProtocol: URLProtocol {
    struct Request {
        let path: String?
        let method: String?
        let capability: String?
        let body: Data?
    }

    static var requests: [Request] = []

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.requests.append(Request(
            path: request.url?.path,
            method: request.httpMethod,
            capability: request.value(forHTTPHeaderField: "X-Tusker-Capability"),
            body: Self.body(of: request)
        ))
        let path = request.url?.path
        let payload: [String: Any]
        switch path {
        case "/api/capability":
            payload = ["capability": "candidate-capability"]
        case "/api/human-receipts/challenge":
            payload = [
                "id": "challenge-1", "project_id": "project-1", "gate_id": "FLW-G-0010", "actor": "human:operator",
                "material_revision": "sha256:material", "action_digest": "sha256:action", "nonce": "nonce-1", "expires_at": "2030-01-02T03:04:05Z",
                "gate_title": "Approve deploy", "action_text": "Deploy revision abc", "verification_text": "Production account owner approval",
            ]
        case "/api/human-receipts/submit":
            payload = ["ok": true]
        default:
            client?.urlProtocol(self, didFailWithError: URLError(.badURL))
            return
        }
        let data = try! JSONSerialization.data(withJSONObject: payload)
        let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}

    private static func body(of request: URLRequest) -> Data? {
        if let data = request.httpBody { return data }
        guard let stream = request.httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while stream.hasBytesAvailable {
            let read = stream.read(&buffer, maxLength: buffer.count)
            guard read > 0 else { break }
            data.append(buffer, count: read)
        }
        return data
    }
}
