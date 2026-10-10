using Godot;

// Quantization declares KHR_mesh_quantization supported: the world's
// normals are normalized bytes, which Godot's accessor decoding reads
// like any other component type; its importer only refuses the file
// because it doesn't know the extension's name (4.7).
public partial class Quantization : GltfDocumentExtension
{
    public override string[] _GetSupportedExtensions() => new[] { "KHR_mesh_quantization" };
}
