package rule

import (
	"fmt"
	"github.com/pkg/errors"
	"log"
	"os/exec"
	"strings"
)

const initConf = `
table ip shield_link {

    set no_vpn_domain_ip_set {
        type ipv4_addr
    }

    set no_vpn_ip_set {
        type ipv4_addr
    }

    chain prerouting {
        type filter hook prerouting priority 0;
    }

    chain select_export {
        ip daddr @no_vpn_ip_set jump wan
        ip daddr @no_vpn_domain_ip_set jump wan
        jump vpn
    }

    chain vpn {
    }

    chain wan {
    }

    chain output {
        type filter hook output priority 0;
        jump wan
    }
}
`

type Interface struct {
	Name   string
	Weight int
}

type OutputInterfaces struct {
	Vpn []Interface
	Wan []Interface
}

func Init() error {
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(initConf)
	return runCmd(cmd)
}

func AddNoVpnDomainIp(ips ...string) error {
	if len(ips) == 0 {
		return nil
	}
	cmd := exec.Command("nft", "add", "element", "ip", "shield_link", "no_vpn_domain_ip_set", fmt.Sprintf("{ %s }", strings.Join(ips, ",")))
	return runCmd(cmd)
}

func DelNoVpnDomainIp(ips ...string) error {
	if len(ips) == 0 {
		return nil
	}
	cmd := exec.Command("nft", "delete", "element", "ip", "shield_link", "no_vpn_domain_ip_set", fmt.Sprintf("{ %s }", strings.Join(ips, ",")))
	return runCmd(cmd)
}

func FlushNoVpnDomainIp() error {
	cmd := exec.Command("nft", "flush", "set", "ip", "shield_link", "no_vpn_domain_ip_set")
	return runCmd(cmd)
}

func AddNoVpnIp(ips ...string) error {
	if len(ips) == 0 {
		return nil
	}
	cmd := exec.Command("nft", "add", "element", "ip", "shield_link", "no_vpn_ip_set", fmt.Sprintf("{ %s }", strings.Join(ips, ",")))
	return runCmd(cmd)
}

func AddControlEthernet(ethers ...string) error {
	for _, ether := range ethers {
		cmd := exec.Command("nft", "add", "rule", "ip", "shield_link", "prerouting", "iifname", fmt.Sprintf("\"%s\"", ether), "jump", "select_export")
		if err := runCmd(cmd); err != nil {
			return err
		}
	}
	return nil
}

var clearIprouteCommands []*exec.Cmd

func clearIpRoutes() error {
	for _, command := range clearIprouteCommands {
		if err := runCmd(command); err != nil {
			log.Printf("clear ip route rule error %+v\n", err)
			continue
		}
	}
	clearIprouteCommands = nil
	return nil
}

func SetOutputInterfaces(value OutputInterfaces) error {
	cmd := exec.Command("nft", "flush", "chain", "ip", "shield_link", "vpn")
	if err := runCmd(cmd); err != nil {
		return err
	}
	cmd = exec.Command("nft", "flush", "chain", "ip", "shield_link", "wan")
	if err := runCmd(cmd); err != nil {
		return err
	}
	n := 0
	markInterfaceMap := map[string]string{}
	set := func(interfaces []Interface, chain string) error {
		if len(interfaces) == 0 {
			return nil
		}
		total := 0
		for _, s := range interfaces {
			weight := s.Weight
			if weight < 1 {
				weight = 1
			}
			total += weight
		}
		current := 0
		betweenArr := make([]string, len(interfaces))
		for i, s := range interfaces {
			n++
			mark := fmt.Sprintf("%x", n+100)
			markInterfaceMap[mark] = s.Name
			start := current
			end := 100
			weight := s.Weight
			if weight < 1 {
				weight = 1
			}
			if i+1 < len(interfaces) {
				end = int((float64(weight) / float64(total)) * 100)
			}
			current = end + 1
			betweenArr[i] = fmt.Sprintf("%d-%d : %s", start, end, mark)
		}
		between := fmt.Sprintf("{ %s }", strings.Join(betweenArr, ", "))
		cmd = exec.Command("nft", "add", "rule", "ip", "shield_link", chain, "meta", "mark", "set", "numgen", "inc", "mod", "100", "map", between)
		if err := runCmd(cmd); err != nil {
			return err
		}
		cmd = exec.Command("nft", "add", "rule", "ip", "shield_link", chain, "ct", "state", "established,related", "meta", "mark", "set", "ct", "mark")
		return runCmd(cmd)
	}
	if err := set(value.Wan, "wan"); err != nil {
		return err
	}
	if err := set(value.Vpn, "vpn"); err != nil {
		return err
	}
	if err := clearIpRoutes(); err != nil {
		return err
	}
	n = 0
	for mark, in := range markInterfaceMap {
		n++
		table := fmt.Sprintf("%d", n+1000)
		cmd = exec.Command("ip", "route", "add", "default", "dev", in, "table", table)
		if err := runCmd(cmd); err != nil {
			return err
		}
		cmd = exec.Command("ip", "rule", "add", "fwmark", mark, "lookup", table)
		if err := runCmd(cmd); err != nil {
			return err
		}
		clearIprouteCommands = append(clearIprouteCommands,
			exec.Command("ip", "route", "del", "default", "dev", in, "table", table),
			exec.Command("ip", "rule", "del", "fwmark", mark, "lookup", table),
		)
	}
	return nil
}

func ClearAll() error {
	if err := clearIpRoutes(); err != nil {
		return err
	}
	cmd := exec.Command("nft", "delete", "table", "ip", "shield_link")
	return runCmd(cmd)
}

func runCmd(cmd *exec.Cmd) error {
	output, err := cmd.CombinedOutput() // 获取命令的输出和错误
	if err != nil {
		return errors.Wrapf(err, "failed to execute nft, output: %s", string(output))
	}
	return nil
}