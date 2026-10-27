package playback

// MediaPaths returns original paths for the selected current and next items.
// Like the rest of Controller it is called only by the serialized owner.
func (c *Controller) MediaPaths() (current, next string) {
	ch := c.channels[c.selected]
	index := 0
	saved := c.saved.ChannelState[ch.Path]
	if saved.FilePath != nil {
		for i, path := range ch.Media {
			if path == *saved.FilePath {
				index = i
				break
			}
		}
	}
	return ch.Media[index], ch.Media[(index+1)%len(ch.Media)]
}
