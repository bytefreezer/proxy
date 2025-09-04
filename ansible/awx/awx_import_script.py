#!/usr/bin/env python3
"""
AWX Import Script for ByteFreezer Proxy
Imports all job templates, workflows, and configurations into AWX

Requirements:
- awx-cli or ansible-runner
- AWX server access
- Proper authentication

Usage:
    python3 awx_import_script.py --server https://your-awx-server --username admin --password password
"""

import argparse
import json
import yaml
import sys
import os
import subprocess
from pathlib import Path

class AWXImporter:
    def __init__(self, server_url, username, password, organization="Default"):
        self.server_url = server_url
        self.username = username
        self.password = password
        self.organization = organization
        self.base_path = Path(__file__).parent
        
    def run_awx_cli(self, resource_type, action, name=None, **kwargs):
        """Execute AWX CLI command"""
        cmd = [
            "awx",
            "--conf.host", self.server_url,
            "--conf.username", self.username,
            "--conf.password", self.password,
            resource_type,
            action
        ]
        
        if name:
            cmd.extend(["--name", name])
            
        for key, value in kwargs.items():
            if value is not None:
                cmd.extend([f"--{key}", str(value)])
        
        try:
            result = subprocess.run(cmd, capture_output=True, text=True, check=True)
            return result.stdout
        except subprocess.CalledProcessError as e:
            print(f"Error executing AWX CLI: {e}")
            print(f"Command: {' '.join(cmd)}")
            print(f"Error output: {e.stderr}")
            return None

    def import_project(self):
        """Import the ByteFreezer Proxy project"""
        print("Importing project...")
        
        result = self.run_awx_cli(
            "project", "create",
            name="ByteFreezer Proxy",
            description="ByteFreezer Proxy Ansible deployment project",
            organization=self.organization,
            scm_type="git",
            scm_url="https://github.com/n0needt0/bytefreezer-proxy.git",
            scm_branch="main",
            scm_clean="true",
            scm_delete_on_update="true",
            scm_update_on_launch="true",
            local_path="ansible"
        )
        
        if result:
            print("✓ Project imported successfully")
        else:
            print("✗ Failed to import project")

    def import_credential(self):
        """Import SSH credential template"""
        print("Creating credential template (requires manual SSH key setup)...")
        
        result = self.run_awx_cli(
            "credential", "create",
            name="ByteFreezer Proxy SSH",
            description="SSH credential for ByteFreezer Proxy servers",
            organization=self.organization,
            credential_type="Machine",
            inputs='{"username": "ubuntu", "become_method": "sudo", "become_username": "root"}'
        )
        
        if result:
            print("✓ Credential template created (add SSH private key manually in AWX UI)")
        else:
            print("✗ Failed to create credential template")

    def import_inventory(self):
        """Import inventory"""
        print("Importing inventory...")
        
        # Create inventory
        result = self.run_awx_cli(
            "inventory", "create",
            name="ByteFreezer Proxy Servers",
            description="Physical servers for ByteFreezer Proxy deployment",
            organization=self.organization
        )
        
        if result:
            print("✓ Inventory created")
            
            # Import inventory source from file
            inventory_file = self.base_path / "inventory_import.yml"
            if inventory_file.exists():
                with open(inventory_file, 'r') as f:
                    inventory_data = yaml.safe_load(f)
                
                print("✓ Inventory structure loaded (configure hosts manually in AWX UI)")
            else:
                print("✗ Inventory import file not found")
        else:
            print("✗ Failed to create inventory")

    def import_job_template(self, template_file):
        """Import a job template from YAML file"""
        template_path = self.base_path / template_file
        
        if not template_path.exists():
            print(f"✗ Template file not found: {template_file}")
            return False
            
        with open(template_path, 'r') as f:
            template_data = yaml.safe_load(f)
        
        template_name = template_data.get('name', 'Unknown')
        print(f"Importing job template: {template_name}")
        
        # Prepare survey spec if present
        survey_spec = template_data.get('survey_spec')
        survey_json = json.dumps(survey_spec) if survey_spec else None
        
        result = self.run_awx_cli(
            "job_template", "create",
            name=template_data.get('name'),
            description=template_data.get('description', ''),
            job_type=template_data.get('job_type', 'run'),
            inventory=template_data.get('inventory'),
            project=template_data.get('project'),
            playbook=template_data.get('playbook'),
            credential=template_data.get('credential'),
            forks=template_data.get('forks', 5),
            limit=template_data.get('limit', ''),
            verbosity=template_data.get('verbosity', 1),
            extra_vars=template_data.get('extra_vars', ''),
            job_tags=template_data.get('job_tags', ''),
            skip_tags=template_data.get('skip_tags', ''),
            timeout=template_data.get('timeout', 0),
            use_fact_cache=template_data.get('use_fact_cache', True),
            ask_scm_branch_on_launch=template_data.get('ask_scm_branch_on_launch', False),
            ask_variables_on_launch=template_data.get('ask_variables_on_launch', False),
            ask_limit_on_launch=template_data.get('ask_limit_on_launch', False),
            ask_tags_on_launch=template_data.get('ask_tags_on_launch', False),
            ask_skip_tags_on_launch=template_data.get('ask_skip_tags_on_launch', False),
            ask_verbosity_on_launch=template_data.get('ask_verbosity_on_launch', False),
            survey_enabled=template_data.get('survey_enabled', False),
            become_enabled=template_data.get('become_enabled', True),
            diff_mode=template_data.get('diff_mode', False),
            allow_simultaneous=template_data.get('allow_simultaneous', False)
        )
        
        if result and survey_json:
            # Add survey spec (requires separate call)
            survey_result = self.run_awx_cli(
                "job_template", "modify", 
                name=template_name,
                survey_spec=survey_json
            )
            
            if survey_result:
                print(f"✓ Job template with survey imported: {template_name}")
            else:
                print(f"✓ Job template imported (survey failed): {template_name}")
        elif result:
            print(f"✓ Job template imported: {template_name}")
        else:
            print(f"✗ Failed to import job template: {template_name}")
            
        return bool(result)

    def import_all_job_templates(self):
        """Import all job templates"""
        template_files = [
            "bytefreezer_proxy_install.yml",
            "bytefreezer_proxy_config_update.yml", 
            "bytefreezer_proxy_service_manage.yml",
            "bytefreezer_proxy_uninstall.yml"
        ]
        
        for template_file in template_files:
            self.import_job_template(template_file)

    def import_workflow_template(self, workflow_file):
        """Import workflow template"""
        workflow_path = self.base_path / workflow_file
        
        if not workflow_path.exists():
            print(f"✗ Workflow file not found: {workflow_file}")
            return False
            
        with open(workflow_path, 'r') as f:
            workflow_data = yaml.safe_load(f)
        
        workflow_name = workflow_data.get('name', 'Unknown')
        print(f"Importing workflow template: {workflow_name}")
        
        # Note: Workflow import is complex and may require manual setup in AWX UI
        print(f"⚠ Workflow templates require manual setup in AWX UI")
        print(f"  Template configuration available in: {workflow_file}")
        
        return True

    def run_import(self):
        """Run complete import process"""
        print("Starting AWX import for ByteFreezer Proxy...")
        print(f"Server: {self.server_url}")
        print(f"Organization: {self.organization}")
        print("-" * 50)
        
        # Import in order
        self.import_project()
        self.import_credential()
        self.import_inventory()
        self.import_all_job_templates()
        self.import_workflow_template("workflow_template_full_deployment.yml")
        
        print("-" * 50)
        print("Import completed!")
        print("\nManual steps required:")
        print("1. Add SSH private key to 'ByteFreezer Proxy SSH' credential")
        print("2. Configure hosts in 'ByteFreezer Proxy Servers' inventory")
        print("3. Set up workflow templates manually (see workflow YAML files)")
        print("4. Test job templates with survey forms")

def main():
    parser = argparse.ArgumentParser(description="Import ByteFreezer Proxy configuration into AWX")
    parser.add_argument("--server", required=True, help="AWX server URL")
    parser.add_argument("--username", required=True, help="AWX username")
    parser.add_argument("--password", required=True, help="AWX password") 
    parser.add_argument("--organization", default="Default", help="AWX organization")
    
    args = parser.parse_args()
    
    # Check if awx-cli is available
    try:
        subprocess.run(["awx", "--version"], capture_output=True, check=True)
    except (subprocess.CalledProcessError, FileNotFoundError):
        print("Error: awx-cli not found. Please install it:")
        print("pip install awxkit")
        sys.exit(1)
    
    importer = AWXImporter(
        args.server,
        args.username, 
        args.password,
        args.organization
    )
    
    importer.run_import()

if __name__ == "__main__":
    main()