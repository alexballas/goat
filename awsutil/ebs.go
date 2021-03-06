package awsutil

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

//EbsVol is a struct defining the discovered EBS volumes and its metadata parsed from the tags
type EbsVol struct {
	EbsVolID     string
	VolumeName   string
	RaidLevel    int
	VolumeSize   int
	AttachedName string
	MountPath    string
	FsType       string
}

//FindEbsVolumes discovers and creates a {'VolumeName':[]EbsVol} map for all the required EBS volumes given an EC2Instance struct
func (e *EC2Instance) FindEbsVolumes() {
	drivesToMount := map[string][]EbsVol{}

	log.Info("Searching for EBS volumes")

	volumes, err := e.findEbsVolumes()
	if err != nil {
		log.Fatalf("Error when searching for EBS volumes: %v", err)
	}

	log.Info("Classifying EBS volumes based on tags")
	for _, volume := range volumes {
		drivesToMount[volume.VolumeName] = append(drivesToMount[volume.VolumeName], volume)
	}

	for volName, volumes := range drivesToMount {
		volGroupLogger := log.WithFields(log.Fields{"vol_name": volName})

		//check for volume mismatch
		volSize := volumes[0].VolumeSize
		mountPath := volumes[0].MountPath
		fsType := volumes[0].FsType
		raidLevel := volumes[0].RaidLevel
		if volSize != -1 {
			if len(volumes) != volSize {
				volGroupLogger.Fatalf("Found %d volumes, expected %d from VolumeSize tag", len(volumes), volSize)
			}
			for _, vol := range volumes[1:] {
				volLogger := log.WithFields(log.Fields{"vol_id": vol.EbsVolID, "vol_name": vol.VolumeName})
				if volSize != vol.VolumeSize || mountPath != vol.MountPath || fsType != vol.FsType || raidLevel != vol.RaidLevel {
					volLogger.Fatal("Mismatched tags among disks of same volume")
				}
			}
		}
	}

	e.Vols = drivesToMount
}

func (e *EC2Instance) findEbsVolumes() ([]EbsVol, error) {
	params := &ec2.DescribeVolumesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("tag:GOAT-IN:Prefix"),
				Values: []*string{
					aws.String(e.Prefix),
				},
			},
			{
				Name: aws.String("tag:GOAT-IN:NodeId"),
				Values: []*string{
					aws.String(e.NodeID),
				},
			},
			{
				Name: aws.String("availability-zone"),
				Values: []*string{
					aws.String(e.Az),
				},
			},
		},
	}

	volumes := []EbsVol{}

	result, err := e.EC2Client.DescribeVolumes(params)
	if err != nil {
		return volumes, err
	}

	for _, volume := range result.Volumes {
		ebsVolume := EbsVol{
			EbsVolID:   *volume.VolumeId,
			VolumeName: "",
			RaidLevel:  -1,
			VolumeSize: -1,
			MountPath:  "",
			FsType:     "",
		}
		if len(volume.Attachments) > 0 {
			for _, attachment := range volume.Attachments {
				if *attachment.InstanceId != e.InstanceID {
					return volumes, fmt.Errorf("Volume %s attached to different instance-id: %s", *volume.VolumeId, *attachment.InstanceId)
				}
				ebsVolume.AttachedName = *attachment.Device
			}
		} else {
			ebsVolume.AttachedName = ""
		}
		for _, tag := range volume.Tags {
			switch *tag.Key {
			case "GOAT-IN:VolumeName":
				ebsVolume.VolumeName = *tag.Value
			case "GOAT-IN:RaidLevel":
				if ebsVolume.RaidLevel, err = strconv.Atoi(*tag.Value); err != nil {
					return volumes, fmt.Errorf("Couldn't parse RaidLevel tag as int: %v", err)
				}
			case "GOAT-IN:VolumeSize":
				if ebsVolume.VolumeSize, err = strconv.Atoi(*tag.Value); err != nil {
					return volumes, fmt.Errorf("Couldn't parse VolumeSize tag as int: %v", err)
				}
			case "GOAT-IN:MountPath":
				ebsVolume.MountPath = *tag.Value
			case "GOAT-IN:FsType":
				ebsVolume.FsType = *tag.Value
			default:
			}
		}
		volumes = append(volumes, ebsVolume)
	}
	return volumes, nil
}
