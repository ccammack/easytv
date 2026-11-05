defmodule MediaTypes do
  @audio_extensions MapSet.new([
                      ".mp3",
                      ".flac",
                      ".m4a",
                      ".m4b",
                      ".aac",
                      ".ac3",
                      ".ogg",
                      ".oga",
                      ".opus",
                      ".wav",
                      ".aiff",
                      ".alac",
                      ".ape",
                      ".amr",
                      ".wma",
                      ".wv",
                      ".tta",
                      ".mp1",
                      ".mp2",
                      ".mpa",
                      ".mid",
                      ".midi",
                      ".xm",
                      ".it",
                      ".s3m",
                      ".mod",
                      ".ra",
                      ".rmi",
                      ".spx",
                      ".voc"
                    ])

  @video_extensions MapSet.new([
                      ".rm",
                      ".mp4",
                      ".m4v",
                      ".mkv",
                      ".mov",
                      ".avi",
                      ".webm",
                      ".flv",
                      ".wmv",
                      ".mpg",
                      ".mpeg",
                      ".mpv",
                      ".m1v",
                      ".m2v",
                      ".ts",
                      ".m2ts",
                      ".mts",
                      ".vob",
                      ".ogv",
                      ".ogm",
                      ".3gp",
                      ".3g2",
                      ".asf",
                      ".divx",
                      ".dv",
                      ".f4v",
                      ".mxf",
                      ".nuv",
                      ".rmvb",
                      ".tod",
                      ".trp",
                      ".vro"
                    ])

  @image_extensions MapSet.new([
                      ".jpg",
                      ".jpeg",
                      ".jpe",
                      ".jfif",
                      ".jif",
                      ".png",
                      ".gif",
                      ".bmp",
                      ".tif",
                      ".tiff",
                      ".webp",
                      ".ico",
                      ".pbm",
                      ".pgm",
                      ".ppm",
                      ".pnm",
                      ".tga"
                    ])

  @media_extensions MapSet.union(@audio_extensions, @video_extensions)
                    |> MapSet.union(@image_extensions)

  def image_extensions, do: @image_extensions
  def media_extensions, do: @media_extensions
end
