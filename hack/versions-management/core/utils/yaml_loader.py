from ruamel.yaml import YAML

def get_yaml_instance() -> YAML:
    yaml = YAML(typ="rt")
    yaml.default_flow_style = False
    yaml.explicit_start = False
    yaml.preserve_quotes = True
    yaml.width = 4096
    yaml.indent(mapping=2, sequence=2, offset=0)
    return yaml
