package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mitchellh/packer/helper/config"
	"github.com/mitchellh/packer/packer"
	"github.com/mitchellh/packer/template/interpolate"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/ovf"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/progress"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

var builtins = map[string]string{
	"mitchellh.virtualbox": "virtualbox",
	"mitchellh.vmware":     "vmware",
	"mitchellh.vmware-esx": "vmware",
}

var virtualboxRe = regexp.MustCompile(`<vssd:VirtualSystemType>virtualbox-(\d)+(\.(\d)+)?<\/vssd:VirtualSystemType>`)
var virtualboxOSRe = regexp.MustCompile(`<OperatingSystemSection ovf:id="(\d)+">`)
var virtualboxNATRe = regexp.MustCompile(`(?s)<Item>(.*?)<\/Item>`)
var virtualboxVboxRe = regexp.MustCompile(`(?s)<vbox:Machine.*<\/vbox:Machine>`)

type OVF struct {
	Datacenter      string `mapstructure:"datacenter"`
	Datastore       string `mapstructure:"datastore"`
	Host            string `mapstructure:"host"`
	Password        string `mapstructure:"password"`
	Username        string `mapstructure:"username"`
	Folder          string `mapstructure:"folder"`
	ResourcePool    string `mapstructure:"resource_pool"`
	VmName          string `mapstructure:"vm_name"`
	OsType          string `mapstructure:"os_type"`
	OsVersion       string `mapstructure:"os_version"`
	OsID            string `mapstructure:"os_id"`
	HardwareVersion string `mapstructure:"hardware_version"`

	ui       packer.Ui
	client   *vim25.Client
	soap     *soap.Client
	path     string
	contents []byte
	envelope *ovf.Envelope

	ctx interpolate.Context
}

func (o *OVF) normalizeOVF(content []byte) {
	newHardwareSectionBase := []byte(`
		<vmw:Config ovf:required="false" vmw:key="tools.afterPowerOn" vmw:value="true"/>
		<vmw:Config ovf:required="false" vmw:key="tools.afterResume" vmw:value="true"/>
		<vmw:Config ovf:required="false" vmw:key="tools.beforeGuestShutdown" vmw:value="true"/>
		<vmw:Config ovf:required="false" vmw:key="tools.beforeGuestStandby" vmw:value="true"/>
	</VirtualHardwareSection>
	`)
	content = bytes.Replace(content, []byte("</VirtualHardwareSection>"), newHardwareSectionBase, -1)
	content = virtualboxNATRe.ReplaceAllFunc(content, func(match []byte) []byte {
		fmt.Println(string(match))
		if bytes.Contains(match, []byte("<rasd:Connection>NAT</rasd:Connection>")) {
			return []byte("")
		}
		return match
	})
	content = virtualboxVboxRe.ReplaceAll(content, []byte(""))
	// make some of this stuff configurable
	var newOSSection string
	if o.OsVersion == "" {
		newOSSection = fmt.Sprintf(`<OperatingSystemSection ovf:id="%s" vmw:osType="%s">`, o.OsID, o.OsType)
	} else {
		newOSSection = fmt.Sprintf(`<OperatingSystemSection ovf:id="%s" ovf:version="%s" vmw:osType="%s">`, o.OsID, o.OsVersion, o.OsType)
	}
	content = virtualboxOSRe.ReplaceAll(content, []byte(newOSSection))
	newHardwareSection := fmt.Sprintf("<vssd:VirtualSystemType>%s</vssd:VirtualSystemType>", o.HardwareVersion)
	o.contents = virtualboxRe.ReplaceAll(content, []byte(newHardwareSection))
}

func (o *OVF) initializeClient() error {
	url := fmt.Sprintf("https://%s:%s@%s/sdk", url.QueryEscape(o.Username), url.QueryEscape(o.Password), o.Host)
	log.Printf("Connecting to vsphere host with connection string %s", url)

	ctx := context.TODO()
	// Set up client
	parsedURL, err := soap.ParseURL(url)
	if err != nil {
		return err
	}
	soapClient := soap.NewClient(parsedURL, true)
	client, err := vim25.NewClient(ctx, soapClient)
	if err != nil {
		return err
	}
	manager := session.NewManager(client)
	err = manager.Login(ctx, parsedURL.User)
	if err != nil {
		return err
	}

	o.client = client
	o.soap = soapClient
	return nil
}

func (o *OVF) parseOVF() error {
	content, err := ioutil.ReadFile(o.path)
	if err != nil {
		return err
	}

	file, err := os.Open(o.path)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := ovf.Unmarshal(file)
	if err != nil {
		return err
	}
	if info == nil {
		return fmt.Errorf("OVF at: %s has an empty envelope", o.path)
	}

	// replace all virtualbox hardware references so that the OVF file works
	o.normalizeOVF(content)
	o.envelope = info
	return nil
}

func (o *OVF) uploadItem(lease *object.HttpNfcLease, ovfItem uploadItem) error {
	item := ovfItem.item
	filePath := item.Path

	if filePath != o.path {
		filePath = filepath.Join(filepath.Dir(o.path), filePath)
	}

	o.ui.Message(fmt.Sprintf("Uploading file: '%s'", filePath))

	log.Printf("Stating file: '%s'", filePath)
	stat, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	log.Printf("Opening file: '%s'", filePath)
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	size := stat.Size()

	// logger := cmd.ProgressLogger(fmt.Sprintf("Uploading %s... ", path.Base(file)))
	// defer logger.Wait()

	opts := soap.Upload{
		ContentLength: size,
		Progress:      ovfItem,
	}

	// Non-disk files (such as .iso) use the PUT method.
	// Overwrite: t header is also required in this case (ovftool does the same)
	if item.Create {
		opts.Method = "PUT"
		opts.Headers = map[string]string{
			"Overwrite": "t",
		}
	} else {
		opts.Method = "POST"
		opts.Type = "application/x-vnd.vmware-streamVmdk"
	}

	log.Printf("Uploading file: '%s' (%d) to '%s' with '%s' method", filePath, size, ovfItem.url.String(), opts.Method)

	return o.soap.Upload(file, ovfItem.url, &opts)
}

func (o *OVF) upload() (*types.ManagedObjectReference, error) {
	ctx := context.TODO()

	name := "Packer uploaded VMDK"
	if o.envelope.VirtualSystem != nil {
		name = o.envelope.VirtualSystem.ID
		if o.envelope.VirtualSystem.Name != nil {
			name = *o.envelope.VirtualSystem.Name
		}
	}

	if o.VmName != "" {
		name = o.VmName
	}

	log.Printf("Configuring import location for '%s'", name)

	finder := find.NewFinder(o.client, true)

	specParams := types.OvfCreateImportSpecParams{
		DiskProvisioning: "thin",
		EntityName:       name,
	}

	log.Printf("Setting finder datacenter to '%s'", o.Datacenter)
	datacenter, err := finder.Datacenter(ctx, o.Datacenter)
	if err != nil {
		return nil, err
	}
	finder.SetDatacenter(datacenter)

	log.Printf("Finding datastore '%s'", o.Datastore)
	datastore, err := finder.Datastore(ctx, o.Datastore)
	if err != nil {
		return nil, err
	}

	log.Printf("Finding folder '%s'", o.Folder)
	folder, err := finder.Folder(ctx, fmt.Sprintf("/%s/vm/%s", o.Datacenter, o.Folder))
	if err != nil {
		return nil, err
	}

	log.Printf("Finding resource pool '%s'", fmt.Sprintf("/%s/host/%s/Resources", o.Datacenter, o.ResourcePool))
	resourcePool, err := finder.ResourcePool(ctx, o.ResourcePool)
	if err != nil {
		return nil, err
	}

	manager := object.NewOvfManager(o.client)

	log.Printf("Creating ovf import spec for '%s'", o.path)
	spec, err := manager.CreateImportSpec(ctx, string(o.contents), resourcePool, datastore, specParams)
	if err != nil {
		return nil, err
	}
	if spec.Error != nil {
		return nil, errors.New(spec.Error[0].LocalizedMessage)
	}
	if spec.Warning != nil {
		for _, w := range spec.Warning {
			log.Printf("Warning: %s\n", w.LocalizedMessage)
		}
	}

	log.Print("Creating import lease for VM")
	lease, err := resourcePool.ImportVApp(ctx, spec.ImportSpec, folder, nil)
	if err != nil {
		return nil, err
	}

	info, err := lease.Wait(ctx)
	if err != nil {
		return nil, err
	}

	// Build slice of items and URLs first, so that the lease updater can know
	// about every item that needs to be uploaded, and thereby infer progress.
	var items []uploadItem

	for _, device := range info.DeviceUrl {
		for _, item := range spec.FileItem {
			if device.ImportKey != item.DeviceId {
				continue
			}

			url, err := o.client.ParseURL(device.Url)
			if err != nil {
				return nil, err
			}

			log.Printf("Creating upload item from '%s' to '%s'", item.Path, url.String())
			item := uploadItem{
				url:  url,
				item: item,
				ch:   make(chan progress.Report),
			}

			items = append(items, item)
		}
	}

	updater := NewUploaderProgress(o.client, lease, items)
	defer updater.Done()

	for _, item := range items {
		err = o.uploadItem(lease, item)
		if err != nil {
			return nil, err
		}
	}

	return &info.Entity, lease.HttpNfcLeaseComplete(ctx)
}

func (o *OVF) HandleOVF(ui packer.Ui, ovfPath string) error {
	o.ui = ui
	o.path = ovfPath

	err := o.initializeClient()
	if err != nil {
		return err
	}
	err = o.parseOVF()
	if err != nil {
		return err
	}

	machine, err := o.upload()
	if err != nil {
		return err
	}

	vm := object.NewVirtualMachine(o.client, *machine)
	vm.MarkAsTemplate(context.TODO())

	return nil
}

type PostProcessor struct {
	config OVF
}

func (p *PostProcessor) Configure(raws ...interface{}) error {
	err := config.Decode(&p.config, &config.DecodeOpts{
		Interpolate: true,
		InterpolateFilter: &interpolate.RenderFilter{
			Exclude: []string{},
		},
	}, raws...)

	if err != nil {
		return err
	}

	if p.config.OsType == "" {
		p.config.OsType = "centos64Guest"
	}

	if p.config.OsID == "" {
		p.config.OsID = "107"
	}

	if p.config.HardwareVersion == "" {
		p.config.HardwareVersion = "vmx-10"
	}

	// Accumulate any errors
	errs := new(packer.MultiError)

	templates := map[string]*string{
		"datacenter":    &p.config.Datacenter,
		"host":          &p.config.Host,
		"password":      &p.config.Password,
		"username":      &p.config.Username,
		"datastore":     &p.config.Datastore,
		"folder":        &p.config.Folder,
		"resource_pool": &p.config.ResourcePool,
	}

	for key, ptr := range templates {
		if *ptr == "" {
			errs = packer.MultiErrorAppend(
				errs, fmt.Errorf("%s must be set", key))
		}
	}

	if len(errs.Errors) > 0 {
		return errs
	}

	return nil
}

func (p *PostProcessor) PostProcess(ui packer.Ui, artifact packer.Artifact) (packer.Artifact, bool, error) {
	if _, ok := builtins[artifact.BuilderId()]; !ok {
		return nil, false, fmt.Errorf("Unknown artifact type, can't build box: %s", artifact.BuilderId())
	}

	ovfPath := ""
	vmdkPath := ""
	for _, path := range artifact.Files() {
		if strings.HasSuffix(path, ".ovf") {
			ovfPath = path
			break
		} else if strings.HasSuffix(path, ".vmdk") {
			vmdkPath = path
		}
	}

	if ovfPath == "" || vmdkPath == "" {
		return nil, false, fmt.Errorf("ERROR: OVF/VMDK were found")
	}

	// only pass the ovf, the vmdk should be referenced in it
	err := p.config.HandleOVF(ui, ovfPath)
	if err != nil {
		return nil, false, err
	}

	return artifact, false, nil
}
