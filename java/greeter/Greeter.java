package greeter;

public class Greeter {
    private final String name;

    public Greeter(String name) {
        this.name = name;
    }

    public String greet() {
        return "Hello, " + name + "!";
    }

    public static void main(String[] args) {
        String name = args.length > 0 ? args[0] : "World";
        Greeter greeter = new Greeter(name);
        System.out.println(greeter.greet());
    }
}
