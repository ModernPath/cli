# /// script
# requires-python = ">=3.10"
# dependencies = ["jsonschema>=4.23,<5"]
# ///
"""Check the real server schema with a standard validator (no production dependency).

From modernpath-core, export with:
  MIX_ENV=test mix run --no-start -e 'IO.puts(Jason.encode!(Core.RDD.ReverseEngineering.Recording.schema()))'
Save the JSON object as schema.json, then run:
  uv run tools/modernpath/test/recording_schema_contract.py schema.json
"""
import copy
import json
import sys

from jsonschema import Draft202012Validator, FormatChecker

schema = json.load(open(sys.argv[1]))
Draft202012Validator.check_schema(schema)
validator = Draft202012Validator(schema, format_checker=FormatChecker())
uuid = "8f5ec2c0-57c1-4a9f-aeb0-a8dfc4d12a9c"
requirement = {"kind": "system_requirement", "id": uuid, "title": "Catalog"}
file_trace = {"source_type": "system_requirement", "source_id": uuid,
              "target_type": "source_file", "repository_key": "main",
              "path": "catalog.ex", "link_kind": "implements"}


def check(name, *, requirements=None, traces=None, valid=True):
    payload = {"requirements": requirements or [], "traces": traces or []}
    errors = list(validator.iter_errors(payload))
    assert bool(errors) != valid, (name, [error.message for error in errors])


check("file link", requirements=[requirement], traces=[file_trace])
for kind in ("user_requirement", "system_requirement"):
    nullable = {**requirement, "kind": kind, "description": None,
                "criteria": [{"kind": "criterion", "statement": "Works", "given": None}]}
    check("nullable content", requirements=[nullable])
    check("nullable criteria", requirements=[{**nullable, "criteria": None}])
check("missing criterion content", requirements=[{**requirement, "criteria": [{"given": None}]}], valid=False)
check("new requirement missing title", requirements=[{"kind": "system_requirement"}], valid=False)
check("nullable file revision", traces=[{**file_trace, "revision": None}])
two_files = {**file_trace, "source_type": "source_file"}
two_files.pop("source_id")
check("two file endpoints", traces=[two_files], valid=False)
for direction in ("source", "target"):
    trace = {"source_type": "system_requirement", "source_id": uuid,
             "target_type": "system_document", "target_integer_id": 1, "link_kind": "relates"}
    if direction == "source":
        trace = {key.replace("source", "TEMP").replace("target", "source").replace("TEMP", "target"): value
                 for key, value in trace.items()}
    check("document relates", traces=[trace])
    for link_kind in ("derives", "implements", "verifies", None):
        invalid = copy.deepcopy(trace)
        if link_kind is None:
            invalid.pop("link_kind")
        else:
            invalid["link_kind"] = link_kind
        check("document relation restriction", traces=[invalid], valid=False)
trace = {"source_type": "system_requirement", "source_id": uuid,
         "target_type": "system_requirement", "target_id": uuid}
for field, value in (("repository_key", "main"), ("path", "catalog.ex"), ("revision", None)):
    check("file metadata requires file endpoint", traces=[{**trace, field: value}], valid=False)
print("Recording JSON Schema contract cases passed")
