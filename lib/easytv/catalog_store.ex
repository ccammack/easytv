defmodule CatalogStore do
  @catalog_file "catalog.json"
  @version 1

  def load! do
    JsonFileStore.load!(
      catalog_path(),
      default_catalog(),
      &validate_catalog!/1,
      "catalog file"
    )
  end

  def save!(catalog_map) when is_map(catalog_map) do
    JsonFileStore.save!(catalog_path(), catalog_map, &validate_catalog!/1)
  end

  def validate_catalog!(%{"version" => @version, "roots" => roots} = data)
      when is_map(roots),
      do: data

  def validate_catalog!(data),
    do: raise("Invalid catalog file schema: #{inspect(data, pretty: true)}")

  def default_catalog, do: %{"version" => @version, "roots" => %{}}
  def catalog_path, do: Script.state_path(@catalog_file)
end
