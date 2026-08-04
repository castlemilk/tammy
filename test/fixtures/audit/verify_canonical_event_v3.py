#!/usr/bin/env python3
"""Independently verify the language-neutral audit canonical-event v3 fixture."""

from __future__ import annotations

import hashlib
import json
import pathlib
import struct
import subprocess


FIXTURE_PATH = pathlib.Path(__file__).with_name("canonical-event-v3.json")


def uint64_be(value: int) -> bytes:
    return struct.pack(">Q", value)


def blinded_framed(domain: str, blinding: bytes, *fields: bytes) -> bytes:
    encoded = bytearray(domain.encode("utf-8"))
    encoded.extend(blinding)
    for field in fields:
        encoded.extend(uint64_be(len(field)))
        encoded.extend(field)
    return bytes(encoded)


def assert_sha256(encoded: bytes, expected_hex: str) -> None:
    python_digest = hashlib.sha256(encoded).digest()
    openssl_digest = subprocess.run(
        ["openssl", "dgst", "-sha256", "-binary"],
        input=encoded,
        check=True,
        capture_output=True,
    ).stdout
    assert python_digest == openssl_digest
    assert python_digest.hex() == expected_hex


def assert_length(field: bytes, expected_hex: str) -> None:
    assert uint64_be(len(field)).hex() == expected_hex


def decode_blinding(commitment: dict[str, object]) -> bytes:
    encoded = commitment["blindingHex"]
    assert isinstance(encoded, str)
    blinding = bytes.fromhex(encoded)
    assert len(blinding) == 32
    assert any(blinding)
    assert blinding.hex() == encoded
    return blinding


def assert_value_commitment(
    commitment: dict[str, object], expected_domain: str
) -> bytes:
    assert commitment["algorithm"] == "SHA-256"
    assert commitment["domainUtf8"] == expected_domain
    assert commitment["inputOrder"] == [
        "domain_utf8",
        "blinding_exact_32_bytes",
        "value_length_uint64_be",
        "value_utf8",
    ]
    value = commitment["valueUtf8"]
    assert isinstance(value, str)
    value_bytes = value.encode("utf-8")
    assert_length(value_bytes, commitment["valueLengthHex"])
    blinding = decode_blinding(commitment)
    assert_sha256(
        blinded_framed(expected_domain, blinding, value_bytes),
        commitment["expectedSha256Hex"],
    )
    return blinding


def main() -> None:
    fixture = json.loads(FIXTURE_PATH.read_text(encoding="utf-8"))
    assert fixture["format"] == "tammy.audit.canonical-event-fixture.v3"
    assert fixture["canonicalEventVersion"] == "tammy.audit.canonical-event.v3"

    hidden = fixture["hiddenMetadataCommitment"]
    assert hidden["algorithm"] == "SHA-256"
    assert hidden["domainUtf8"] == "tammy-audit-hidden-metadata-commitment-v2"
    assert hidden["inputOrder"] == [
        "domain_utf8",
        "blinding_exact_32_bytes",
        "canonical_metadata_length_uint64_be",
        "canonical_metadata_utf8",
    ]
    hidden_blinding = decode_blinding(hidden)
    hidden_json = hidden["canonicalMetadataUtf8"].encode("utf-8")
    assert json.dumps(json.loads(hidden_json), sort_keys=True, separators=(",", ":")) == hidden_json.decode("utf-8")
    assert_length(hidden_json, hidden["canonicalMetadataLengthHex"])
    assert_sha256(
        blinded_framed(hidden["domainUtf8"], hidden_blinding, hidden_json),
        hidden["expectedSha256Hex"],
    )

    payload = fixture["payloadCommitment"]
    assert payload["algorithm"] == "SHA-256"
    assert payload["domainUtf8"] == "tammy-audit-payload-identity-commitment-v1"
    assert payload["inputOrder"] == [
        "domain_utf8",
        "blinding_exact_32_bytes",
        "type_url_length_uint64_be",
        "type_url_utf8",
        "schema_fingerprint_length_uint64_be",
        "schema_fingerprint_exact_32_bytes",
        "payload_proto_length_uint64_be",
        "payload_proto_exact_bytes",
        "payload_json_length_uint64_be",
        "payload_json_canonical_utf8",
    ]
    payload_blinding = decode_blinding(payload)
    type_url = payload["typeUrlUtf8"].encode("utf-8")
    fingerprint = bytes.fromhex(payload["schemaFingerprintHex"])
    payload_proto = bytes.fromhex(payload["payloadProtoHex"])
    payload_json = payload["payloadJsonUtf8"].encode("utf-8")
    assert len(fingerprint) == 32
    assert json.dumps(json.loads(payload_json), sort_keys=True, separators=(",", ":")) == payload_json.decode("utf-8")
    for field, key in (
        (type_url, "typeUrlLengthHex"),
        (fingerprint, "schemaFingerprintLengthHex"),
        (payload_proto, "payloadProtoLengthHex"),
        (payload_json, "payloadJsonLengthHex"),
    ):
        assert_length(field, payload[key])
    assert_sha256(
        blinded_framed(
            payload["domainUtf8"],
            payload_blinding,
            type_url,
            fingerprint,
            payload_proto,
            payload_json,
        ),
        payload["expectedSha256Hex"],
    )

    event_type = fixture["eventTypeCommitment"]
    occurred_at = fixture["occurredAtCommitment"]
    actor_user_id = fixture["actorUserIdCommitment"]
    event_type_blinding = assert_value_commitment(
        event_type, "tammy-audit-event-type-commitment-v1"
    )
    occurred_at_blinding = assert_value_commitment(
        occurred_at, "tammy-audit-occurred-at-commitment-v1"
    )
    actor_user_id_blinding = assert_value_commitment(
        actor_user_id, "tammy-audit-actor-user-id-commitment-v1"
    )
    assert event_type["valueUtf8"] == "1"
    assert occurred_at["valueUtf8"] == "2026-08-04T01:02:03Z"
    assert actor_user_id["valueUtf8"] == ""
    assert len(
        {
            hidden_blinding,
            payload_blinding,
            event_type_blinding,
            occurred_at_blinding,
            actor_user_id_blinding,
        }
    ) == 5

    envelope = fixture["canonicalEnvelopeUtf8"].encode("utf-8")
    parsed_envelope = json.loads(envelope)
    assert json.dumps(parsed_envelope, sort_keys=True, separators=(",", ":")) == envelope.decode("utf-8")
    assert set(parsed_envelope) == {
        "actor_user_id_commitment",
        "event_type_commitment",
        "hidden_metadata_commitment",
        "identity_projection",
        "occurred_at_commitment",
        "payload_identity_commitment",
        "version",
    }
    assert set(parsed_envelope["identity_projection"]) == {
        "generation",
        "sequence",
        "workspace_id",
    }
    assert parsed_envelope["identity_projection"] == {
        "generation": "1",
        "sequence": "1",
        "workspace_id": "01890f60-4d6d-7c12-8f02-6c9129d5b001",
    }
    assert parsed_envelope["version"] == fixture["canonicalEventVersion"]
    assert parsed_envelope["hidden_metadata_commitment"] == hidden["expectedSha256Hex"]
    assert parsed_envelope["payload_identity_commitment"] == payload["expectedSha256Hex"]
    assert parsed_envelope["event_type_commitment"] == event_type["expectedSha256Hex"]
    assert parsed_envelope["occurred_at_commitment"] == occurred_at["expectedSha256Hex"]
    assert parsed_envelope["actor_user_id_commitment"] == actor_user_id["expectedSha256Hex"]

    framing = fixture["framing"]
    assert framing["algorithm"] == "SHA-256"
    assert framing["domainUtf8"] == "tammy-audit-event-v3"
    assert framing["canonicalLengthEncoding"] == "uint64-big-endian"
    assert framing["inputOrder"] == [
        "domain_utf8",
        "predecessor_32_bytes",
        "canonical_length_uint64_be",
        "canonical_envelope_utf8",
    ]
    assert_length(envelope, framing["canonicalLengthHex"])
    predecessor = bytes.fromhex(fixture["predecessorHex"])
    assert len(predecessor) == 32
    event_input = (
        framing["domainUtf8"].encode("utf-8")
        + predecessor
        + uint64_be(len(envelope))
        + envelope
    )
    assert_sha256(event_input, fixture["expectedEventSha256Hex"])
    print("canonical-event-v3 fixture verified with Python hashlib and OpenSSL")


if __name__ == "__main__":
    main()
