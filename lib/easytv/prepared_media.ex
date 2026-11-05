defmodule PreparedMedia do
  require Logger

  @max_image_dimension 1920
  @keep_last_files 4

  def prepare_for_playback(path) when is_binary(path) do
    abspath = Path.expand(path)

    if image_file?(abspath) do
      prepare_image_for_display(abspath, @max_image_dimension)
    else
      {:ok, %{original_path: abspath, play_path: abspath, tmp?: false, downscaled?: false}}
    end
  end

  def image_file?(path) when is_binary(path) do
    path
    |> Path.extname()
    |> String.downcase()
    |> then(&MapSet.member?(MediaTypes.image_extensions(), &1))
  end

  def image_file?(_), do: false

  defp prepare_image_for_display(path, maxdim) do
    tmp_dir = Script.tmp_dir("prepared-media")

    result =
      case copy_image_to_tmp_source(path, tmp_dir) do
        {:ok, tmp_source} ->
          with {:ok, w, h} <- image_dimensions(tmp_source) do
            if max(w, h) > maxdim do
              case downscale_to_output(tmp_source, tmp_dir, maxdim) do
                {:ok, tmp_output} ->
                  {:ok,
                   %{
                     original_path: path,
                     play_path: tmp_output,
                     tmp?: true,
                     downscaled?: true
                   }}

                {:error, reason} ->
                  {:error, reason}
              end
            else
              {:ok,
               %{
                 original_path: path,
                 play_path: tmp_source,
                 tmp?: true,
                 downscaled?: false
               }}
            end
          end

        {:error, reason} ->
          {:error, reason}
      end

    maybe_start_async_sweep(tmp_dir, @keep_last_files)
    result
  end

  defp copy_image_to_tmp_source(path, tmp_dir) do
    ext =
      path
      |> Path.extname()
      |> String.downcase()

    tmp_source =
      Path.join(
        tmp_dir,
        "src-#{System.system_time(:millisecond)}-#{System.unique_integer([:positive])}#{ext}"
      )

    Logger.debug(
      "PreparedMedia copying image source=#{inspect(path)} tmp_source=#{inspect(tmp_source)}"
    )

    case File.cp(path, tmp_source) do
      :ok -> {:ok, tmp_source}
      {:error, reason} -> {:error, {:file_copy_failed, path, tmp_source, reason}}
    end
  end

  defp image_dimensions(path) do
    vipsheader = System.find_executable("vipsheader")

    if is_nil(vipsheader) do
      {:error, :vipsheader_not_found}
    else
      with {:ok, w} <- vipsheader_int(vipsheader, "width", path),
           {:ok, h} <- vipsheader_int(vipsheader, "height", path) do
        {:ok, w, h}
      end
    end
  end

  defp vipsheader_int(vipsheader, field, path) do
    {output, status} =
      System.cmd(vipsheader, ["-f", field, path], stderr_to_stdout: true)

    case {status, String.trim(output)} do
      {0, value} ->
        case Integer.parse(value) do
          {n, ""} -> {:ok, n}
          _ -> {:error, {:bad_vipsheader_output, field, value}}
        end

      _ ->
        {:error, {:vipsheader_failed, field, output}}
    end
  end

  defp downscale_to_output(tmp_source, tmp_dir, max_dim) do
    vipsthumbnail = System.find_executable("vipsthumbnail")

    if is_nil(vipsthumbnail) do
      {:error, :vipsthumbnail_not_found}
    else
      tmp_output =
        Path.join(
          tmp_dir,
          "out-#{System.system_time(:millisecond)}-#{System.unique_integer([:positive])}.jpg"
        )

      {output, status} =
        System.cmd(
          vipsthumbnail,
          [
            tmp_source,
            "--size",
            "#{max_dim}x#{max_dim}",
            "--linear",
            "-o",
            "#{tmp_output}[Q=90]"
          ],
          stderr_to_stdout: true
        )

      case status do
        0 ->
          if File.regular?(tmp_output) do
            {:ok, tmp_output}
          else
            {:error, {:vipsthumbnail_missing_output, output}}
          end

        _ ->
          {:error, {:vipsthumbnail_failed, output}}
      end
    end
  end

  defp maybe_start_async_sweep(dir, keep_last) do
    Task.start(fn ->
      sweep_tmp_dir(dir, keep_last)
    end)

    :ok
  end

  defp sweep_tmp_dir(dir, keep_last)
       when is_binary(dir) and is_integer(keep_last) and keep_last >= 0 do
    case File.ls(dir) do
      {:ok, entries} ->
        entries
        |> Enum.map(&Path.join(dir, &1))
        |> Enum.filter(&File.regular?/1)
        |> Enum.sort_by(&mtime/1, :desc)
        |> Enum.drop(keep_last)
        |> Enum.each(fn path ->
          case File.rm(path) do
            :ok ->
              :ok

            {:error, :enoent} ->
              :ok

            {:error, reason} ->
              Logger.debug(
                "PreparedMedia sweep delete failed path=#{inspect(path)} reason=#{inspect(reason)}"
              )
          end
        end)

        :ok

      {:error, reason} ->
        Logger.debug(
          "PreparedMedia sweep list failed dir=#{inspect(dir)} reason=#{inspect(reason)}"
        )

        :ok
    end
  end

  defp mtime(path) do
    case File.stat(path) do
      {:ok, %File.Stat{mtime: mtime}} -> mtime
      _ -> {{1970, 1, 1}, {0, 0, 0}}
    end
  end
end
