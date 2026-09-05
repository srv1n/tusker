import AppKit
import CryptoKit
import Foundation
import LocalAuthentication
import Security

enum HumanReceiptError: LocalizedError {
    case unsupportedRuntime
    case unownedRuntime
    case invalidRequest
    case invalidChallenge
    case expired
    case refused
    case server

    var errorDescription: String? {
        switch self {
        case .unsupportedRuntime: return "Human approvals require TuskerBar's managed local runtime."
        case .unownedRuntime: return "Human approvals require the TuskerBar-launched runtime bound to this local address."
        case .invalidRequest, .invalidChallenge: return "The approval request is invalid."
        case .expired: return "The approval request expired."
        case .refused: return "The approval was not recorded."
        case .server: return "Tusker could not record the approval."
        }
    }
}

struct HumanReceiptRequest: Equatable {
    let projectID: String
    let gateID: String
    let action: String

    init(projectID: String, gateID: String, action: String) throws {
        guard Self.validIdentifier(projectID), Self.validIdentifier(gateID), ["satisfy", "waive", "obsolete"].contains(action) else {
            throw HumanReceiptError.invalidRequest
        }
        self.projectID = projectID
        self.gateID = gateID
        self.action = action
    }

    private static func validIdentifier(_ value: String) -> Bool {
        guard !value.isEmpty, value.count <= 160 else { return false }
        return value.unicodeScalars.allSatisfy {
            switch $0.value {
            case 45, 46, 48 ... 57, 58, 65 ... 90, 95, 97 ... 122: return true
            default: return false
            }
        }
    }
}

struct HumanReceiptChallenge: Decodable, Equatable {
    let id: String
    let projectID: String
    let gateID: String
    let actor: String
    let materialRevision: String
    let actionDigest: String
    let nonce: String
    let expiresAt: String
    let gateTitle: String
    let actionText: String
    let verificationText: String

    enum CodingKeys: String, CodingKey {
        case id, actor, nonce
        case projectID = "project_id"
        case gateID = "gate_id"
        case materialRevision = "material_revision"
        case actionDigest = "action_digest"
        case expiresAt = "expires_at"
        case gateTitle = "gate_title"
        case actionText = "action_text"
        case verificationText = "verification_text"
    }

    func validate(for request: HumanReceiptRequest, now: Date = .now) throws -> Date {
        guard projectID == request.projectID, gateID == request.gateID,
              [id, projectID, gateID, actor, materialRevision, actionDigest, nonce, expiresAt].allSatisfy(HumanDecisionReceipt.isCanonicalField),
              Self.isDisplayText(gateTitle, limit: 4_096), Self.isDisplayText(actionText, limit: 16_384), Self.isDisplayText(verificationText, limit: 16_384),
              let expiry = HumanDecisionReceipt.parseTimestamp(expiresAt) else {
            throw HumanReceiptError.invalidChallenge
        }
        guard expiry > now else { throw HumanReceiptError.expired }
        return expiry
    }

    private static func isDisplayText(_ value: String, limit: Int) -> Bool {
        !value.isEmpty && value.count <= limit && value.unicodeScalars.allSatisfy { $0.value >= 0x20 || $0 == "\n".unicodeScalars.first! }
    }
}

struct HumanDecisionReceipt: Codable, Equatable {
    static let schema = "tusker.human-receipt/v1"

    let challengeID: String
    let projectID: String
    let gateID: String
    let actor: String
    let materialRevision: String
    let actionDigest: String
    let answer: String
    let nonce: String
    let issuedAt: String
    let expiresAt: String
    let keyID: String

    enum CodingKeys: String, CodingKey {
        case challengeID = "challenge_id"
        case projectID = "project_id"
        case gateID = "gate_id"
        case actor, answer, nonce
        case materialRevision = "material_revision"
        case actionDigest = "action_digest"
        case issuedAt = "issued_at"
        case expiresAt = "expires_at"
        case keyID = "key_id"
    }

    init(challenge: HumanReceiptChallenge, issuedAt: String, keyID: String) throws {
        let fields = [challenge.id, challenge.projectID, challenge.gateID, challenge.actor, challenge.materialRevision, challenge.actionDigest, "accept", challenge.nonce, issuedAt, challenge.expiresAt, keyID]
        guard fields.allSatisfy(Self.isCanonicalField) else { throw HumanReceiptError.invalidChallenge }
        challengeID = challenge.id
        projectID = challenge.projectID
        gateID = challenge.gateID
        actor = challenge.actor
        materialRevision = challenge.materialRevision
        actionDigest = challenge.actionDigest
        answer = "accept"
        nonce = challenge.nonce
        self.issuedAt = issuedAt
        expiresAt = challenge.expiresAt
        self.keyID = keyID
    }

    func canonicalData() throws -> Data {
        let fields = [Self.schema, challengeID, projectID, gateID, actor, materialRevision, actionDigest, answer, nonce, issuedAt, expiresAt]
        guard fields.allSatisfy(Self.isCanonicalField) else { throw HumanReceiptError.invalidChallenge }
        return Data(fields.joined(separator: "\n").utf8)
    }

    static func timestamp(_ date: Date = .now) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.string(from: date)
    }

    static func parseTimestamp(_ value: String) -> Date? {
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = fractional.date(from: value) { return date }
        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]
        return plain.date(from: value)
    }

    static func isCanonicalField(_ value: String) -> Bool {
        !value.isEmpty && value.count <= 4096 && value.unicodeScalars.allSatisfy { $0.value >= 0x20 && $0.value != 0x7f }
    }
}

enum HumanReceiptApproval {
    static func accepts(_ response: NSApplication.ModalResponse) -> Bool {
        response == .alertFirstButtonReturn
    }
}

enum HumanReceiptNativeOperator {
    static func resolve(environment: [String: String], username: String = NSUserName()) -> String? {
        for value in [environment["TUSKER_SERVE_OPERATOR"], environment["TUSKER_ACTOR"]] {
            if let actor = canonicalHumanActor(value) { return actor }
        }
        return canonicalHumanActor("human:" + username)
    }

    private static func canonicalHumanActor(_ raw: String?) -> String? {
        guard let raw else { return nil }
        let parts = raw.trimmingCharacters(in: .whitespacesAndNewlines).split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
        guard parts.count == 2,
              parts[0].trimmingCharacters(in: .whitespacesAndNewlines).lowercased() == "human" else { return nil }
        let name = parts[1].trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty ? nil : "human:" + name
    }
}

final class HumanReceiptNativeKey {
    static let shared = HumanReceiptNativeKey()

    private static let service = "com.tusker.human-receipt"
    private static let account = "secure-enclave-p256-v1"

    private init() {}

    func publicKeyBase64() throws -> String {
        try publicKeyDER().base64EncodedString().trimmingCharacters(in: CharacterSet(charactersIn: "="))
    }

    func keyID() throws -> String {
        "sha256:" + SHA256.hash(data: try publicKeyDER()).map { String(format: "%02x", $0) }.joined()
    }

    func sign(_ data: Data) throws -> Data {
        try privateKey(authenticationContext: LAContext()).signature(for: data).derRepresentation
    }

    private func publicKeyDER() throws -> Data {
        try privateKey(authenticationContext: nil).publicKey.derRepresentation
    }

    private func privateKey(authenticationContext: LAContext?) throws -> SecureEnclave.P256.Signing.PrivateKey {
        if let data = try storedKeyReference() {
            return try SecureEnclave.P256.Signing.PrivateKey(dataRepresentation: data, authenticationContext: authenticationContext)
        }
        let access = SecAccessControlCreateWithFlags(nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly, [.privateKeyUsage, .userPresence], nil)!
        let key = try SecureEnclave.P256.Signing.PrivateKey(accessControl: access, authenticationContext: authenticationContext)
        try storeKeyReference(key.dataRepresentation)
        return key
    }

    private func storedKeyReference() throws -> Data? {
        let query: [CFString: Any] = [
            kSecClass: kSecClassGenericPassword,
            kSecAttrService: Self.service,
            kSecAttrAccount: Self.account,
            kSecReturnData: true,
        ]
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess, let data = result as? Data else { throw HumanReceiptError.refused }
        return data
    }

    private func storeKeyReference(_ data: Data) throws {
        let item: [CFString: Any] = [
            kSecClass: kSecClassGenericPassword,
            kSecAttrService: Self.service,
            kSecAttrAccount: Self.account,
            kSecAttrAccessible: kSecAttrAccessibleWhenUnlockedThisDeviceOnly,
            kSecValueData: data,
        ]
        guard SecItemAdd(item as CFDictionary, nil) == errSecSuccess else { throw HumanReceiptError.refused }
    }
}

private struct HumanReceiptChallengeRequest: Encodable {
    let projectID: String
    let gateID: String
    let action: String

    enum CodingKeys: String, CodingKey {
        case projectID = "projectId"
        case gateID = "gateId"
        case action
    }
}

private struct HumanReceiptSubmitRequest: Encodable {
    let projectID: String
    let receipt: HumanDecisionReceipt
    let signature: String

    enum CodingKeys: String, CodingKey {
        case projectID = "projectId"
        case receipt, signature
    }
}

struct HumanReceiptCapability: Decodable {
    let capability: String
}

struct HumanReceiptSubmitResult: Decodable {
    let ok: Bool?
}

struct HumanReceiptSubmission: Encodable {
    let receipt: HumanDecisionReceipt
    let signature: String
}

@MainActor
final class HumanReceiptTransport {
    private let baseURL: URL
    private let session: URLSession
    private var cachedCapability: String?

    init(baseURL: URL, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.session = session
    }

    func fetchChallenge(_ request: HumanReceiptRequest) async throws -> HumanReceiptChallenge {
        let url = baseURL.appendingPathComponent("api/human-receipts/challenge")
        var urlRequest = URLRequest(url: url)
        urlRequest.httpMethod = "POST"
        urlRequest.setValue("application/json", forHTTPHeaderField: "Content-Type")
        urlRequest.setValue(try await capability(), forHTTPHeaderField: "X-Tusker-Capability")
        urlRequest.httpBody = try JSONEncoder().encode(HumanReceiptChallengeRequest(projectID: request.projectID, gateID: request.gateID, action: request.action))
        let (data, response) = try await session.data(for: urlRequest)
        guard let http = response as? HTTPURLResponse, (200 ..< 300).contains(http.statusCode) else { throw HumanReceiptError.server }
        return try JSONDecoder().decode(HumanReceiptChallenge.self, from: data)
    }

    func submit(_ result: HumanReceiptSubmission) async throws {
        let url = baseURL.appendingPathComponent("api/human-receipts/submit")
        var urlRequest = URLRequest(url: url)
        urlRequest.httpMethod = "POST"
        urlRequest.setValue("application/json", forHTTPHeaderField: "Content-Type")
        urlRequest.setValue(try await capability(), forHTTPHeaderField: "X-Tusker-Capability")
        urlRequest.httpBody = try JSONEncoder().encode(HumanReceiptSubmitRequest(projectID: result.receipt.projectID, receipt: result.receipt, signature: result.signature))
        let (data, response) = try await session.data(for: urlRequest)
        guard let http = response as? HTTPURLResponse, (200 ..< 300).contains(http.statusCode),
              try JSONDecoder().decode(HumanReceiptSubmitResult.self, from: data).ok == true else { throw HumanReceiptError.server }
    }

    private func capability() async throws -> String {
        if let cachedCapability { return cachedCapability }
        let (data, response) = try await session.data(from: baseURL.appendingPathComponent("api/capability"))
        guard let http = response as? HTTPURLResponse, (200 ..< 300).contains(http.statusCode),
              let capability = try JSONDecoder().decode(HumanReceiptCapability.self, from: data).capability.nilIfEmpty else {
            throw HumanReceiptError.server
        }
        cachedCapability = capability
        return capability
    }
}

struct HumanReceiptBridgeResult: Encodable {
    let status: String
    let message: String?

    static let accepted = Self(status: "accepted", message: nil)
    static let cancelled = Self(status: "cancelled", message: nil)
    static func error(_ error: Error) -> Self { Self(status: "error", message: error.localizedDescription) }
}

@MainActor
final class HumanDecisionReceiptController {
    static let shared = HumanDecisionReceiptController()

    private init() {}

    func request(_ request: HumanReceiptRequest, baseURL: URL, presenting window: NSWindow) async -> HumanReceiptBridgeResult {
        do {
            guard RuntimeLaunchPlan.manages(baseURL) else { throw HumanReceiptError.unsupportedRuntime }
            guard RuntimeSupervisor.shared.ownsHumanReceiptRuntime(at: baseURL) else { throw HumanReceiptError.unownedRuntime }
            let transport = HumanReceiptTransport(baseURL: baseURL)
            let challenge = try await transport.fetchChallenge(request)
            let expiry = try challenge.validate(for: request)
            guard await confirm(challenge, presenting: window) else { return .cancelled }
            guard RuntimeSupervisor.shared.ownsHumanReceiptRuntime(at: baseURL) else { throw HumanReceiptError.unownedRuntime }
            let issuedAt = Date.now
            guard expiry > issuedAt else { throw HumanReceiptError.expired }
            let receipt = try HumanDecisionReceipt(challenge: challenge, issuedAt: HumanDecisionReceipt.timestamp(issuedAt), keyID: HumanReceiptNativeKey.shared.keyID())
            let signature = try HumanReceiptNativeKey.shared.sign(receipt.canonicalData()).base64EncodedString().trimmingCharacters(in: CharacterSet(charactersIn: "="))
            try await transport.submit(HumanReceiptSubmission(receipt: receipt, signature: signature))
            return .accepted
        } catch HumanReceiptError.refused {
            return .cancelled
        } catch {
            presentFailure(error)
            return .error(error)
        }
    }

    private func confirm(_ challenge: HumanReceiptChallenge, presenting window: NSWindow) async -> Bool {
        let alert = NSAlert()
        alert.messageText = "Approve this Tusker decision?"
        alert.informativeText = "This is a one-time approval. Tusker will reject it after \(challenge.expiresAt)."
        alert.alertStyle = .warning
        alert.addButton(withTitle: "Approve")
        alert.addButton(withTitle: "Cancel")
        alert.accessoryView = receiptDetailsView(challenge)
        return await withCheckedContinuation { continuation in
            alert.beginSheetModal(for: window) { response in continuation.resume(returning: HumanReceiptApproval.accepts(response)) }
        }
    }

    private func receiptDetailsView(_ challenge: HumanReceiptChallenge) -> NSView {
        let text = """
        Gate: \(challenge.gateTitle)

        Action: \(challenge.actionText)

        Verification: \(challenge.verificationText)

        Operator: \(challenge.actor)
        Project: \(challenge.projectID)
        Gate ID: \(challenge.gateID)
        Material revision: \(challenge.materialRevision)
        Action digest: \(challenge.actionDigest)
        """
        let scroll = NSScrollView(frame: NSRect(x: 0, y: 0, width: 500, height: 230))
        scroll.hasVerticalScroller = true
        scroll.borderType = .bezelBorder
        let view = NSTextView(frame: scroll.bounds)
        view.isEditable = false
        view.isSelectable = true
        view.string = text
        view.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
        view.textColor = .labelColor
        view.backgroundColor = .textBackgroundColor
        scroll.documentView = view
        return scroll
    }

    private func presentFailure(_ error: Error) {
        let alert = NSAlert(error: error)
        alert.runModal()
    }
}

private extension String {
    var nilIfEmpty: String? { isEmpty ? nil : self }
}
