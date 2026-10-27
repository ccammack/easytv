package main

func playerOutputArgs(headless bool) []string {
	if headless {
		return []string{"--ao=null", "--vo=null"}
	}
	return []string{"--force-window=yes", "--fullscreen", "--title=easytv", "--osd-level=0", "--cache=no", "--profile=fast", "--hwdec=auto"}
}
