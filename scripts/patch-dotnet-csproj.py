#!/usr/bin/env python3
"""Patch generated .NET SDK metadata for public package publishing."""

from __future__ import annotations

import sys
import xml.etree.ElementTree as ET
from pathlib import Path


METADATA = {
    "Authors": "John Flavan",
    "Company": "Community Maintained",
    "PackageLicenseExpression": "MIT",
    "PackageReadmeFile": "README.md",
    "PackageTags": "pulumi;osano;consent;cookie-consent;privacy;cmp",
    # The generator targets net6.0, which is out of support; the package requires .NET 8 or later, as
    # the docs state and tests/dotnet/SdkCompatibility.csproj checks.
    "TargetFramework": "net8.0",
    # The generator enables the XML documentation file only for Debug builds; ship it in Release too.
    "GenerateDocumentationFile": "true",
    # The generated Args and Result classes have no XML comments; CS1591 would report each one.
    "NoWarn": "$(NoWarn);CS1591",
}


def set_child(parent: ET.Element, tag: str, text: str) -> None:
    child = parent.find(tag)
    if child is None:
        child = ET.SubElement(parent, tag)
    child.text = text


def ensure_readme_item(root: ET.Element) -> None:
    for group in root.findall("ItemGroup"):
        readme = group.find("None[@Include='README.md']")
        if readme is None:
            continue
        set_child(readme, "Pack", "True")
        set_child(readme, "PackagePath", "\\")
        return

    group = ET.Element("ItemGroup")
    readme = ET.SubElement(group, "None", {"Include": "README.md"})
    ET.SubElement(readme, "Pack").text = "True"
    ET.SubElement(readme, "PackagePath").text = "\\"

    insert_at = len(root)
    for index, child in enumerate(list(root)):
        plugin = child.find("EmbeddedResource[@Include='pulumi-plugin.json']")
        if plugin is not None:
            insert_at = index
            break
    root.insert(insert_at, group)


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: patch-dotnet-csproj.py <path>", file=sys.stderr)
        return 1

    path = Path(sys.argv[1])
    tree = ET.parse(path)
    root = tree.getroot()

    property_group = root.find("PropertyGroup")
    if property_group is None:
        raise RuntimeError("expected a PropertyGroup in generated csproj")

    for tag, text in METADATA.items():
        set_child(property_group, tag, text)

    ensure_readme_item(root)

    ET.indent(tree, space="  ")
    tree.write(path, encoding="utf-8", xml_declaration=False)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
