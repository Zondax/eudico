#! /bin/bash

tmux \
    new-session 'EUDICO_PATH=$PWD/data/alice ./eudico  delegated daemon --genesis=gen.gen; sleep infinity' \; \
    split-window -h 'EUDICO_PATH=$PWD/data/bob ./eudico  delegated daemon --genesis=gen.gen; sleep infinity' \; \
    split-window 'EUDICO_PATH=$PWD/data/bob ./eudico wait-api; EUDICO_PATH=$PWD/data/bob ./eudico log set-level error; EUDICO_PATH=$PWD/data/bob ./eudico net connect /ip4/127.0.0.1/tcp/3000/p2p/12D3KooWLikVfeSrcMUi9vkxWTsu1AYXx73Vc6Gdpg3pF5t2jnWF; sleep 3' \; \
    split-window -h 'EUDICO_PATH=$PWD/data/charlie ./eudico  delegated daemon --genesis=gen.gen; sleep infinity' \; \
    split-window 'EUDICO_PATH=$PWD/data/charlie ./eudico wait-api; EUDICO_PATH=$PWD/data/charlie ./eudico log set-level error; EUDICO_PATH=$PWD/data/charlie ./eudico net connect /ip4/127.0.0.1/tcp/3000/p2p/12D3KooWLikVfeSrcMUi9vkxWTsu1AYXx73Vc6Gdpg3pF5t2jnWF /ip4/127.0.0.1/tcp/3001/p2p/12D3KooWDV63brKnzfXWMSZEGfs328qYwRKi4BJGCJTiB18NKB8X; sleep 3' \; \
    select-pane -t 0 \; \
    split-window -v 'EUDICO_PATH=$PWD/data/alice ./eudico wait-api; EUDICO_PATH=$PWD/data/alice ./eudico log set-level error; EUDICO_PATH=$PWD/data/alice ./eudico wallet import --as-default --format=json-lotus kek.key; EUDICO_PATH=$PWD/data/alice ./eudico delegated miner; sleep infinity' \;