// Downloads torrents from the command-line.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/anacrolix/envpprof"
	"github.com/anacrolix/tagflag"
	"github.com/dustin/go-humanize"
	"github.com/gosuri/uiprogress"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

func resolvedPeerAddrs(ss []string) (ret []torrent.Peer, err error) {
	for _, s := range ss {
		var addr *net.TCPAddr
		addr, err = net.ResolveTCPAddr("tcp", s)
		if err != nil {
			return
		}
		ret = append(ret, torrent.Peer{
			IP:   addr.IP,
			Port: addr.Port,
		})
	}
	return
}

func torrentBar(t *torrent.Torrent) {
	bar := uiprogress.AddBar(1)
	bar.AppendCompleted()
	bar.AppendFunc(func(*uiprogress.Bar) (ret string) {
		select {
		case <-t.GotInfo():
		default:
			return "getting info"
		}
		if t.Seeding() {
			return "seeding"
		} else if t.BytesCompleted() == t.Info().TotalLength() {
			return "completed"
		} else {
			return fmt.Sprintf("downloading (%s/%s)", humanize.Bytes(uint64(t.BytesCompleted())), humanize.Bytes(uint64(t.Info().TotalLength())))
		}
	})
	bar.PrependFunc(func(*uiprogress.Bar) string {
		return t.Name()
	})
	go func() {
		<-t.GotInfo()
		bar.Total = int(t.Info().TotalLength())
		for {
			bc := t.BytesCompleted()
			bar.Set(int(bc))
			time.Sleep(time.Second)
		}
	}()
}

func addTorrents(client *torrent.Client) {
	for _, arg := range flags.Torrent {
		t := func() *torrent.Torrent {
			if strings.HasPrefix(arg, "magnet:") {
				t, err := client.AddMagnet(arg)
				if err != nil {
					log.Fatalf("error adding magnet: %s", err)
				}
				return t
			} else {
				metaInfo, err := metainfo.LoadFromFile(arg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error loading torrent file %q: %s\n", arg, err)
					os.Exit(1)
				}
				t, err := client.AddTorrent(metaInfo)
				if err != nil {
					log.Fatal(err)
				}
				return t
			}
		}()
		torrentBar(t)
		t.AddPeers(func() (ret []torrent.Peer) {
			for _, ta := range flags.TestPeer {
				ret = append(ret, torrent.Peer{
					IP:   ta.IP,
					Port: ta.Port,
				})
			}
			return
		}())
		go func() {
			<-t.GotInfo()
			t.DownloadAll()
		}()
	}
}

var flags struct {
	Mmap     bool           `help:"memory-map torrent data"`
	TestPeer []*net.TCPAddr `short:"p" help:"addresses of some starting peers"`
	Seed     bool           `help:"seed after download is complete"`
	Addr     *net.TCPAddr   `help:"network listen addr"`
	tagflag.StartPos
	Torrent []string `arity:"+" help:"torrent file path or magnet uri"`
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	tagflag.Parse(&flags)
	var clientConfig torrent.Config
	if flags.Mmap {
		clientConfig.DefaultStorage = storage.NewMMap("")
	}
	if flags.Addr != nil {
		clientConfig.ListenAddr = flags.Addr.String()
	}

	client, err := torrent.NewClient(&clientConfig)
	if err != nil {
		log.Fatalf("error creating client: %s", err)
	}
	defer client.Close()
	// Write status on the root path on the default HTTP muxer. This will be
	// bound to localhost somewhere if GOPPROF is set, thanks to the envpprof
	// import.
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		client.WriteStatus(w)
	})
	uiprogress.Start()
	addTorrents(client)
	if client.WaitAll() {
		log.Print("downloaded ALL the torrents")
	} else {
		log.Fatal("y u no complete torrents?!")
	}
	if flags.Seed {
		select {}
	}
}
