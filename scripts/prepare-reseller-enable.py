#!/usr/bin/env python3
import argparse
import json
import os
import re

import yaml


class UniqueLoader(yaml.SafeLoader):
    pass


def unique_mapping(loader, node, deep=False):
    result = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node, deep=deep)
        if key in result:
            raise ValueError("Duplicate YAML configuration key")
        result[key] = loader.construct_object(value_node, deep=deep)
    return result


UniqueLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, unique_mapping)


def normalize_host(value):
    if not isinstance(value, str):
        raise ValueError("Host entries must be strings")
    host = value.strip().lower()
    if host == "::1":
        return host
    if len(host) > 253 or any(not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", label) for label in host.split(".")):
        raise ValueError("Hosts must not contain a scheme, port, path or wildcard")
    return host


def prepare(source, extra_hosts, container_env):
    overrides = [item.split("=", 1)[0] for item in container_env if item.startswith("RESELLER_")]
    if overrides:
        raise ValueError("Review container environment overrides before enabling reseller mode: " + ", ".join(overrides))
    config = yaml.load(source, Loader=UniqueLoader)
    if not isinstance(config, dict):
        raise ValueError("Configuration must be a YAML mapping")
    section = config.get("reseller", {})
    if not isinstance(section, dict):
        raise ValueError("reseller must be a YAML mapping")
    if section.get("enabled", False) is not False:
        raise ValueError("Reseller mode is already enabled or enabled is not a boolean; review manually")
    if not isinstance(section.get("main_hosts", []), list):
        raise ValueError("main_hosts must be a list")
    hosts = list(dict.fromkeys(normalize_host(host) for host in section.get("main_hosts", []) + extra_hosts))
    required = {"share.aimosh.com", "moshskmgr.aimosh.com", "localhost", "127.0.0.1", "::1", "dujiaonext"}
    if not required.issubset(hosts):
        raise ValueError("Required main, admin, loopback or internal service hosts are missing")
    candidate_section = dict(section)
    candidate_section.update(enabled=True, main_hosts=hosts, trusted_forwarded_host=False, self_apply_enabled=True)
    candidate_section.setdefault("subdomain_base", "aimosh.com")
    if not candidate_section["subdomain_base"]:
        candidate_section["subdomain_base"] = "aimosh.com"
    normalize_host(candidate_section["subdomain_base"])
    candidate_section.setdefault("settlement_confirm_days", 7)
    candidate = dict(config, reseller=candidate_section)
    result = yaml.safe_dump(candidate, allow_unicode=True, sort_keys=False)
    if yaml.safe_load(result) != candidate:
        raise ValueError("YAML round-trip verification failed")
    summary = {"enabled_before": False, "enabled_candidate": True, "main_hosts": hosts,
               "trusted_forwarded_host": False, "self_apply_enabled": True,
               "subdomain_base": candidate_section["subdomain_base"],
               "settlement_confirm_days": candidate_section["settlement_confirm_days"]}
    return result, summary


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", required=True)
    parser.add_argument("--candidate", required=True)
    parser.add_argument("--container-env", required=True)
    parser.add_argument("--main-host", action="append", default=[])
    args = parser.parse_args()
    with open(args.source, encoding="utf-8") as stream:
        source = stream.read()
    with open(args.container_env, encoding="utf-8") as stream:
        environment = json.load(stream)
    if not isinstance(environment, list) or not all(isinstance(item, str) for item in environment):
        raise ValueError("Invalid container environment inventory")
    result, summary = prepare(source, args.main_host, environment)
    fd = os.open(args.candidate, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        stream.write(result)
    print(json.dumps(summary, ensure_ascii=False))


if __name__ == "__main__":
    try:
        main()
    except ValueError as error:
        raise SystemExit(str(error) + ". No live configuration was changed.")
    except (OSError, yaml.YAMLError):
        raise SystemExit("Configuration preparation failed; check YAML, host inventory and environment overrides. No live configuration was changed.")
