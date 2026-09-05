#!/usr/bin/env python3
"""Patch local item entries into Python 2.3 bytecode.

The legacy client imports object/itemCFG.pyc directly. Updating only the source
therefore leaves shop descriptions unavailable even when Commodity.ini contains
the product. This build-time tool edits the portable Python 2.3 code object with
xdis and preserves the original bytecode timestamp. Local room-control cards
remain present in the complete client item cache so categorized shop tabs,
ordinary backpacks and room-start rules share the same artwork, name and
description. No UI bytecode is rewritten.
"""

from __future__ import annotations

import argparse
import os
import pathlib
import struct
import sys


SOLO_BOSS_ID = 30098
SOLO_BOSS_RESOURCE_ID = 99
SOLO_BOSS_NAME = "单人BOSS卡"
SOLO_BOSS_DESCRIPTION = "持有且未收藏时，满足BOSS挑战条件即可单人开始竞技BOSS战。"
SOLO_BOSS_LEGACY_DESCRIPTIONS = (
    "持有此卡且满足BOSS挑战条件时，可以单人开始竞技BOSS战。",
)
COMPETITIVE_AI_ID = 30099
COMPETITIVE_AI_NAME = "AI对战卡"
COMPETITIVE_AI_DESCRIPTION = "持有且未收藏时，普通竞技开局自动补充AI对手。自由场补满可用位置；组队房只有一队且剩余位置足够时补充等人数对手，否则不能开始。AI只在本局出现。"
COMPETITIVE_AI_LEGACY_DESCRIPTIONS = (
    "放在背包时，符合条件的普通竞技房会自动补充AI对手；放入收藏柜即可关闭。",
    "持有且未收藏时，普通竞技开局自动补充AI对手。自由场补满可用位置；组队房只有一队时补充等人数对手。AI只在本局出现。",
)

def configure_xdis(pytools: pathlib.Path) -> None:
    if not pytools.is_dir():
        raise RuntimeError(f"bundled xdis directory is missing: {pytools}")
    sys.path.insert(0, str(pytools))


def emit(opmap: dict[str, int], name: str, argument: int | None = None) -> bytes:
    opcode = opmap[name]
    if argument is None:
        return bytes((opcode,))
    if not 0 <= argument <= 0xFFFF:
        raise RuntimeError(f"{name} argument {argument} exceeds Python 2.3 wordcode")
    return bytes((opcode,)) + struct.pack("<H", argument)


class Marshal23Scanner:
    """Offset-only reader for the marshal types used by CPython 2.3 pyc files."""

    def __init__(self, data: bytes):
        self.data = data

    def int32(self, offset: int) -> int:
        return struct.unpack_from("<i", self.data, offset)[0]

    def skip(self, offset: int) -> int:
        tag = chr(self.data[offset])
        offset += 1
        if tag in "0NFTS.":
            return offset
        if tag in "iR":
            return offset + 4
        if tag == "I":
            return offset + 8
        if tag == "l":
            digits = self.int32(offset)
            return offset + 4 + abs(digits) * 2
        if tag == "f":
            return offset + 1 + self.data[offset]
        if tag == "g":
            return offset + 8
        if tag == "x":
            for _ in range(2):
                offset += 1 + self.data[offset]
            return offset
        if tag == "y":
            return offset + 16
        if tag in "stu":
            size = self.int32(offset)
            return offset + 4 + size
        if tag in "([":
            count = self.int32(offset)
            offset += 4
            for _ in range(count):
                offset = self.skip(offset)
            return offset
        if tag == "{":
            while chr(self.data[offset]) != "0":
                offset = self.skip(offset)
                offset = self.skip(offset)
            return offset + 1
        if tag == "c":
            offset += 16
            for _ in range(8):
                offset = self.skip(offset)
            offset += 4
            return self.skip(offset)
        raise RuntimeError(f"unsupported Python 2.3 marshal tag {tag!r} at 0x{offset - 1:X}")


def marshal_value(value: object) -> bytes:
    if isinstance(value, int):
        return b"i" + struct.pack("<i", value)
    if isinstance(value, str):
        value = value.encode("ascii")
    if isinstance(value, bytes):
        return b"s" + struct.pack("<i", len(value)) + value
    raise RuntimeError(f"unsupported injected constant {value!r}")


def migrate_marshaled_string(path: pathlib.Path, legacy_values: tuple[bytes, ...], replacement: bytes) -> bool:
    """Replace a known string constant without changing bytecode indexes."""
    from xdis.load import load_module

    original = path.read_bytes()
    replacement_marshaled = marshal_value(replacement)
    rewritten = original
    changed = False
    for legacy in legacy_values:
        marker = marshal_value(legacy)
        if marker in rewritten:
            rewritten = rewritten.replace(marker, replacement_marshaled, 1)
            changed = True
            break
    if not changed:
        return False
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_bytes(rewritten)
    try:
        version, *_rest = load_module(str(temporary))
        if version != (2, 3):
            raise RuntimeError(f"rewritten bytecode verification failed: {temporary}")
        os.replace(temporary, path)
    finally:
        if temporary.exists():
            temporary.unlink()
    return True


def rewrite_python23_module(path: pathlib.Path, store_offset: int, inserted_code: bytes, values: tuple[object, ...]) -> None:
    from xdis.load import load_module

    original = path.read_bytes()
    scanner = Marshal23Scanner(original)
    if len(original) < 30 or original[8:9] != b"c":
        raise RuntimeError(f"{path} is not a Python 2.3 module code object")
    code_object_offset = 8 + 1 + 16
    if original[code_object_offset : code_object_offset + 1] != b"s":
        raise RuntimeError(f"{path} co_code is not a marshalled string")
    code_size = scanner.int32(code_object_offset + 1)
    code_data_offset = code_object_offset + 5
    code_end = code_data_offset + code_size
    constants_offset = code_end
    if original[constants_offset : constants_offset + 1] != b"(":
        raise RuntimeError(f"{path} co_consts is not a marshalled tuple")
    constants_count = scanner.int32(constants_offset + 1)
    constants_end = scanner.skip(constants_offset)
    if constants_count > 0xFFFF - len(values):
        raise RuntimeError(f"{path} has too many constants for Python 2.3 wordcode")

    patched_code = original[code_data_offset : code_data_offset + store_offset] + inserted_code + original[code_data_offset + store_offset : code_end]
    patched_constants = (
        b"("
        + struct.pack("<i", constants_count + len(values))
        + original[constants_offset + 5 : constants_end]
        + b"".join(marshal_value(value) for value in values)
    )
    rewritten = (
        original[:code_object_offset]
        + b"s"
        + struct.pack("<i", len(patched_code))
        + patched_code
        + patched_constants
        + original[constants_end:]
    )
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_bytes(rewritten)
    try:
        version, _timestamp, _magic, verified, *_rest = load_module(str(temporary))
        if version != (2, 3) or len(verified.co_code) != len(patched_code):
            raise RuntimeError(f"rewritten bytecode verification failed: {temporary}")
        os.replace(temporary, path)
    finally:
        if temporary.exists():
            temporary.unlink()


def patch_dictionary_pyc(path: pathlib.Path, dictionary_name: str, item_id: int, tuple_values: tuple[object, ...], check: bool) -> bool:
    from xdis.bytecode import Bytecode
    from xdis.load import load_module
    from xdis.op_imports import get_opcode_module

    version, _timestamp, _magic_int, code, _is_pypy, _source_size, _sip_hash = load_module(str(path))
    if version != (2, 3):
        raise RuntimeError(f"{path} uses Python {version}, expected Python 2.3")
    opcode = get_opcode_module(version)
    instructions = list(Bytecode(code, opcode))
    store = next(
        (instruction for instruction in instructions if instruction.opname == "STORE_NAME" and instruction.argval == dictionary_name),
        None,
    )
    if store is None:
        raise RuntimeError(f"{path} has no STORE_NAME for {dictionary_name}")
    already_present = any(
        instruction.offset < store.offset
        and instruction.opname == "LOAD_CONST"
        and instruction.argval == item_id
        for instruction in instructions
    )
    if already_present:
        return False
    if check:
        raise RuntimeError(f"{path} does not contain item {item_id}")

    injected_values = (item_id,) + tuple_values
    key_index = len(code.co_consts)
    value_indices = list(range(key_index + 1, key_index + len(injected_values)))
    inserted = bytearray(emit(opcode.opmap, "DUP_TOP"))
    inserted.extend(emit(opcode.opmap, "LOAD_CONST", key_index))
    for index in value_indices:
        inserted.extend(emit(opcode.opmap, "LOAD_CONST", index))
    inserted.extend(emit(opcode.opmap, "BUILD_TUPLE", len(value_indices)))
    inserted.extend(emit(opcode.opmap, "ROT_THREE"))
    inserted.extend(emit(opcode.opmap, "STORE_SUBSCR"))

    rewrite_python23_module(path, store.offset, bytes(inserted), injected_values)
    return True


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--client-root", required=True, type=pathlib.Path)
    parser.add_argument("--xdis-root", type=pathlib.Path)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    client_root = args.client_root.resolve()
    xdis_root = args.xdis_root.resolve() if args.xdis_root else client_root.parent / "pytools"
    configure_xdis(xdis_root)

    name = SOLO_BOSS_NAME.encode("gbk")
    description = SOLO_BOSS_DESCRIPTION.encode("gbk")
    legacy_solo_boss_descriptions = tuple(value.encode("gbk") for value in SOLO_BOSS_LEGACY_DESCRIPTIONS)
    ai_name = COMPETITIVE_AI_NAME.encode("gbk")
    ai_description = COMPETITIVE_AI_DESCRIPTION.encode("gbk")
    legacy_ai_descriptions = tuple(value.encode("gbk") for value in COMPETITIVE_AI_LEGACY_DESCRIPTIONS)
    changes = []
    for registry in (client_root / "object" / "itemCFG.pyc", client_root / "object" / "commodityCFG.pyc"):
        changes.append(migrate_marshaled_string(registry, legacy_solo_boss_descriptions, description))
        changes.append(migrate_marshaled_string(registry, legacy_ai_descriptions, ai_description))
    entries = (
        (SOLO_BOSS_ID, name, description),
        (COMPETITIVE_AI_ID, ai_name, ai_description),
    )
    for item_id, item_name, item_description in entries:
        changes.append(
            patch_dictionary_pyc(
                client_root / "object" / "itemCFG.pyc",
                "itemList",
                item_id,
                ("item", SOLO_BOSS_RESOURCE_ID, item_name, item_description, "", 0),
                args.check,
            )
        )
        changes.append(
            patch_dictionary_pyc(
                client_root / "object" / "commodityCFG.pyc",
                "commodityList",
                item_id,
                ("item", SOLO_BOSS_RESOURCE_ID, item_name, item_description),
                args.check,
            )
        )
    if not args.check:
        print(f"patched Python 2.3 item registries: {sum(changes)} file(s) changed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
