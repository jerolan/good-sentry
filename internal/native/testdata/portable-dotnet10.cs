using System;
internal static class Program
{
    private static int Compute(int value)
    {
#line 100 "alpha.cs"
        var doubled = value * 2;
#line hidden
        doubled++;
#line 200 "beta.cs"
        return doubled + 1;
#line default
    }
    private static void Main() => Console.WriteLine(Compute(7));
}
