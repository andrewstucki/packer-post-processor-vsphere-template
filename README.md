#VSphere Template Post Processor

This post-processor creates VSphere templates from Virtualbox packer builders. It expects the VBoxManage
output step to create OVF files.

##Compatibility

So far this has been tested with Virtualbox 5.1.6, VSphere running ESXI 5.5, and packer 0.10.1. I see no
reason why other combinations won't work as well.

##Installing

Download your platform-specific binary from the [latest releases page](https://github.com/andrewstucki/packer-post-processor-vsphere-template/releases/latest). Either
put your binary somewhere accessible on your path or reference the absolute path to the binary in
your `$HOME/.packerconfig` file, like this:

    {
      "post-processors": {
        "vsphere-template": "/path/to/packer-post-processor-vsphere-template"
      }
    }

Use the post-processor as follows in your packer manifest:

    "post-processors": [
      {
        "type":            "vsphere-template",
        "host":            "{{user `vsphere_host`}}",
        "username":        "{{user `vsphere_username`}}",
        "password":        "{{user `vsphere_password`}}",
        "datacenter":      "{{user `vsphere_datacenter`}}",
        "resource_pool":   "{{user `vsphere_resource_pool`}}",
        "folder":          "{{user `vsphere_folder`}}",
        "datastore":       "{{user `vsphere_datastore`}}"
      }
    ]

The following attributes are available:

| Attribute        | Description                                                             | Required/Optional |
| ---------------- | ----------------------------------------------------------------------- | ----------------- |
| host             | VCenter host for API calls                                              | required          |
| username         | Username to use for auth                                                | required          |
| password         | Password to use for auth                                                | required          |
| datacenter       | The datacenter where the template will be deployed                      | required          |
| resource_pool    | The resource pool for the template                                      | required          |
| folder           | The vm folder to place the template in                                  | required          |
| datastore        | The datastore to back the template disk                                 | required          |
| os_type          | The VMWare os type to inject into the OVF (defaults to centos64Guest)   | optional          |
| os_id            | The VMWare os id to inject into the OVF template (defaults to 107)      | optional          |
| hardware_version | The VMWare hardware version to inject into the OVF (defaults to vmx-10) | optional          |
