defmodule EasyTV.UserConfig.Loader do
  def load!(path) do
    Code.require_file(Path.expand(path))

    unless function_exported?(EasyTV.UserConfig, :config, 0) do
      raise ArgumentError, "config file must define EasyTV.UserConfig.config/0: #{path}"
    end

    config = apply(EasyTV.UserConfig, :config, [])

    unless is_list(config) and Keyword.keyword?(config) do
      raise ArgumentError, "config must return a keyword list: #{path}"
    end

    for key <- [:on_key, :on_cec, :on_mpv] do
      unless is_function(Keyword.fetch!(config, key), 2) do
        raise ArgumentError, "#{key} must be a two-argument callback: #{path}"
      end
    end

    %{
      state: Keyword.fetch!(config, :state),
      on_key: Keyword.fetch!(config, :on_key),
      on_cec: Keyword.fetch!(config, :on_cec),
      on_mpv: Keyword.fetch!(config, :on_mpv)
    }
  end
end
