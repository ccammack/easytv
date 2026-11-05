defmodule DirectoryPlaylistLoader do
  require Logger

  def scan_root(root_dir) do
    root_dir = Path.expand(root_dir)

    case File.ls(root_dir) do
      {:ok, entries} ->
        channels =
          entries
          |> Enum.reject(&String.starts_with?(&1, "."))
          |> Enum.map(&Path.join(root_dir, &1))
          |> Enum.filter(&File.dir?/1)
          |> Enum.sort()
          |> Enum.reduce(%{}, fn channel_path, acc ->
            channel_name = Path.basename(channel_path)

            media =
              channel_path
              |> list_files_recursive()
              |> Enum.filter(&media_file?/1)
              |> Enum.sort()

            if media == [] do
              acc
            else
              Map.put(acc, channel_path, %{
                "name" => channel_name,
                "path" => channel_path,
                "media" => media
              })
            end
          end)

        {:ok, channels}

      {:error, reason} ->
        Logger.warning("Could not list root directory #{root_dir}: #{inspect(reason)}")
        {:error, reason}
    end
  end

  defp list_files_recursive(path) do
    cond do
      File.regular?(path) ->
        [path]

      File.dir?(path) ->
        case File.ls(path) do
          {:ok, entries} ->
            entries
            |> Enum.reject(&String.starts_with?(&1, "."))
            |> Enum.map(&Path.join(path, &1))
            |> Enum.flat_map(&list_files_recursive/1)

          {:error, reason} ->
            Logger.warning("Could not list directory #{path}: #{inspect(reason)}")
            []
        end

      true ->
        []
    end
  end

  defp media_file?(path) do
    path
    |> Path.extname()
    |> String.downcase()
    |> then(&MapSet.member?(MediaTypes.media_extensions(), &1))
  end
end
