// Package adder implements functionality to add content to IPFS daemons
// managed by the Cluster.
package adder

import (
	"context"
	"fmt"
	"mime/multipart"
	"strings"

	"github.com/ipfs/ipfs-cluster/adder/ipfsadd"
	"github.com/ipfs/ipfs-cluster/api"

	cid "gx/ipfs/QmPSQnBKM9g7BaUcZCvswUJVscQ1ipjmwxN5PXCjkp9EQ7/go-cid"
	files "gx/ipfs/QmPhx9B9cuaXc4vuw62567BF5NxfpsdD1AVE9HbTn7t1Y6/go-ipfs-files"
	multihash "gx/ipfs/QmPnFwZ2JXKnXgMw8CdBPxn7FWh6LLdjUjxV1fKHuJnkr8/go-multihash"
	ipld "gx/ipfs/QmR7TcHkR9nxkUorfi8XMTAMLUK7GiP64TWWBzY3aacc1o/go-ipld-format"
	merkledag "gx/ipfs/QmSei8kFMfqdJq7Q68d2LMnHbTWKKg2daA29ezUYFAUNgc/go-merkledag"
	logging "gx/ipfs/QmZChCsSt8DctjceaL56Eibc29CVQq4dGKRXC5JRZ6Ppae/go-log"
)

var logger = logging.Logger("adder")

// ClusterDAGService is an implementation of ipld.DAGService plus a Finalize
// method. ClusterDAGServices can be used to provide Adders with a different
// add implementation.
type ClusterDAGService interface {
	ipld.DAGService
	// Finalize receives the IPFS content root CID as
	// returned by the ipfs adder.
	Finalize(ctx context.Context, ipfsRoot cid.Cid) (cid.Cid, error)
}

// Adder is used to add content to IPFS Cluster using an implementation of
// ClusterDAGService.
type Adder struct {
	ctx    context.Context
	cancel context.CancelFunc

	dgs ClusterDAGService

	params *api.AddParams

	// AddedOutput updates are placed on this channel
	// whenever a block is processed. They contain information
	// about the block, the CID, the Name etc. and are mostly
	// meant to be streamed back to the user.
	output chan *api.AddedOutput
}

// New returns a new Adder with the given ClusterDAGService, add options and a
// channel to send updates during the adding process.
//
// An Adder may only be used once.
func New(ds ClusterDAGService, p *api.AddParams, out chan *api.AddedOutput) *Adder {
	// Discard all progress update output as the caller has not provided
	// a channel for them to listen on.
	if out == nil {
		out = make(chan *api.AddedOutput, 100)
		go func() {
			for range out {
			}
		}()
	}

	return &Adder{
		dgs:    ds,
		params: p,
		output: out,
	}
}

func (a *Adder) setContext(ctx context.Context) {
	if a.ctx == nil { // only allows first context
		ctxc, cancel := context.WithCancel(ctx)
		a.ctx = ctxc
		a.cancel = cancel
	}
}

// FromMultipart adds content from a multipart.Reader. The adder will
// no longer be usable after calling this method.
func (a *Adder) FromMultipart(ctx context.Context, r *multipart.Reader) (cid.Cid, error) {
	logger.Debugf("adding from multipart with params: %+v", a.params)

	f, err := files.NewFileFromPartReader(r, "multipart/form-data")
	if err != nil {
		return cid.Undef, err
	}
	defer f.Close()
	return a.FromFiles(ctx, f)
}

// FromFiles adds content from a files.Directory. The adder will no longer
// be usable after calling this method.
func (a *Adder) FromFiles(ctx context.Context, f files.Directory) (cid.Cid, error) {
	logger.Debugf("adding from files")
	a.setContext(ctx)

	if a.ctx.Err() != nil { // don't allow running twice
		return cid.Undef, a.ctx.Err()
	}

	defer a.cancel()
	defer close(a.output)

	ipfsAdder, err := ipfsadd.NewAdder(a.ctx, a.dgs)
	if err != nil {
		logger.Error(err)
		return cid.Undef, err
	}

	ipfsAdder.Hidden = a.params.Hidden
	ipfsAdder.Trickle = a.params.Layout == "trickle"
	ipfsAdder.RawLeaves = a.params.RawLeaves
	ipfsAdder.Wrap = a.params.Wrap
	ipfsAdder.Chunker = a.params.Chunker
	ipfsAdder.Out = a.output
	ipfsAdder.Progress = a.params.Progress

	// Set up prefix
	prefix, err := merkledag.PrefixForCidVersion(a.params.CidVersion)
	if err != nil {
		return cid.Undef, fmt.Errorf("bad CID Version: %s", err)
	}

	hashFunCode, ok := multihash.Names[strings.ToLower(a.params.HashFun)]
	if !ok {
		return cid.Undef, fmt.Errorf("unrecognized hash function: %s", a.params.HashFun)
	}
	prefix.MhType = hashFunCode
	prefix.MhLength = -1
	ipfsAdder.CidBuilder = &prefix

	it := f.Entries()
	for it.Next() {
		select {
		case <-a.ctx.Done():
			return cid.Undef, a.ctx.Err()
		default:
			logger.Debugf("ipfsAdder AddFile(%s)", it.Name())

			if ipfsAdder.AddFile(it.Name(), it.Node()); err != nil {
				logger.Error("error adding to cluster: ", err)
				return cid.Undef, err
			}
		}
	}
	if it.Err() != nil {
		return cid.Undef, it.Err()
	}

	adderRoot, err := ipfsAdder.Finalize()
	if err != nil {
		return cid.Undef, err
	}
	clusterRoot, err := a.dgs.Finalize(a.ctx, adderRoot.Cid())
	if err != nil {
		logger.Error("error finalizing adder:", err)
		return cid.Undef, err
	}
	logger.Infof("%s successfully added to cluster", clusterRoot)
	return clusterRoot, nil
}
