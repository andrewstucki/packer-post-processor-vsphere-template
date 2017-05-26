VSphere Template Post Processor
===============================

This post-processor creates VSphere templates from Virtualbox packer builders. It expects the VBoxManage
output step to create OVF files.

Compatibility
-------------

So far this has been tested with Virtualbox 5.1.6, VSphere running ESXI 5.5 & 6.5, and packer 0.10.1 & 1.0.0. I see no
reason why other combinations won't work as well.

Installing
----------

Download your platform-specific binary from the [latest releases page](https://github.com/andrewstucki/packer-post-processor-vsphere-template/releases/latest). Either
put your binary somewhere accessible on your path or reference the absolute path to the binary in
your `$HOME/.packerconfig` file, like this:
```
{
  "post-processors": {
    "vsphere-template": "/path/to/packer-post-processor-vsphere-template"
  }
}
```

Use the post-processor as follows in your packer manifest:
```
"post-processors": [
  {
    "type":            "vsphere-template",
    "host":            "{{user `vsphere_host`}}",
    "username":        "{{user `vsphere_username`}}",
    "password":        "{{user `vsphere_password`}}",
    "datacenter":      "{{user `vsphere_datacenter`}}",
    "datastore":       "{{user `vsphere_datastore`}}"
  }
]
```

The following attributes are available:

| Attribute        | Description                                                                            | Required/Optional |
| ---------------- | -------------------------------------------------------------------------------------- | ----------------- |
| host             | VCenter host for API calls                                                             | required          |
| username         | Username to use for auth                                                               | required          |
| password         | Password to use for auth                                                               | required          |
| datacenter       | The datacenter where the template will be deployed                                     | required          |
| datastore        | The datastore to back the template disk                                                | required          |
| resource_pool    | The resource pool for the template (defaults to the default datacenter resource pool)  | optional          |
| folder           | The vm folder to place the template in (defaults to the root of the datacenter)        | optional          |
| os_type          | The VMWare os type to inject into the OVF (defaults to centos64Guest)                  | optional          |
| os_id            | The VMWare os id to inject into the OVF template (defaults to 107)                     | optional          |
| os_version       | The VMWare os version to inject into the OVF template (defaults to "")                 | optional          |
| vm_name          | The name of the OVF template to upload (defaults to the builder name)                  | optional          |
| hardware_version | The VMWare hardware version to inject into the OVF (defaults to vmx-10)                | optional          |
