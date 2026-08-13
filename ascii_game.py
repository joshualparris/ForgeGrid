import sys
import random

def main():
    print("================================")
    print("   ASCII DUNGEON ESCAPE!        ")
    print("================================")
    print("You wake up in a cold, dark dungeon.")
    print("You see two doors: Left and Right.")
    
    health = 10
    
    while health > 0:
        print(f"\n[HP: {health}]")
        try:
            choice = input("Which door do you choose? (l/r): ").strip().lower()
        except EOFError:
            print("\nEOF reached, exiting.")
            break
            
        if choice not in ['l', 'r']:
            print("Invalid choice. Type 'l' or 'r'.")
            continue
            
        event = random.choice(['monster', 'treasure', 'empty', 'exit'])
        
        if event == 'monster':
            dmg = random.randint(2, 5)
            health -= dmg
            print(f"Ah! A goblin attacks you! You lose {dmg} HP.")
        elif event == 'treasure':
            heal = random.randint(1, 3)
            health += heal
            print(f"You found a potion! You restore {heal} HP.")
        elif event == 'empty':
            print("The room is empty... just cobwebs.")
        elif event == 'exit':
            print("You found the stairs leading up to the surface!")
            print("YOU ESCAPED THE DUNGEON! YOU WIN!")
            return
            
    if health <= 0:
        print("You succumb to your injuries... GAME OVER.")

if __name__ == "__main__":
    main()
