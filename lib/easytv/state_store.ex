defmodule StateStore do
  @state_file "state.json"
  @version 1

  def load! do
    JsonFileStore.load!(
      state_path(),
      default_state(),
      &validate_state!/1,
      "state file"
    )
  end

  def save!(state_map) when is_map(state_map) do
    JsonFileStore.save!(state_path(), state_map, &validate_state!/1)
  end

  def validate_state!(
        %{
          "version" => @version,
          "current_channel_id" => current_channel_id,
          "channel_state" => channel_state
        } = data
      )
      when (is_binary(current_channel_id) or is_nil(current_channel_id)) and is_map(channel_state),
      do: data

  def validate_state!(data),
    do: raise("Invalid state file schema: #{inspect(data, pretty: true)}")

  def default_state,
    do: %{"version" => @version, "current_channel_id" => nil, "channel_state" => %{}}

  def state_path, do: Script.state_path(@state_file)
end
