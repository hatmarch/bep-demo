package greeter;

import static org.junit.Assert.assertEquals;
import org.junit.Test;

public class GreeterTest {
    @Test
    public void testGreetDefault() {
        Greeter greeter = new Greeter("World");
        assertEquals("Hello, World!", greeter.greet());
    }

    @Test
    public void testGreetCustomName() {
        Greeter greeter = new Greeter("Alice");
        assertEquals("Hello, Alice!", greeter.greet());
    }

    @Test
    public void testGreetEmptyName() {
        Greeter greeter = new Greeter("");
        assertEquals("Hello, !", greeter.greet());
    }
}
